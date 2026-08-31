package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"symagios/encrypt"
	"symagios/proto/manifest"
	"symagios/storage"
	"symagios/utils/types"
)

// applyNextAction handles a structured directive from a post-LLM reply.
// AGENT|SKILL|SUB_AGENT: if the capability is on a local workflow manifest →
// Pending (this node's engine). Otherwise WaitingForExternal* so agent-gateway
// can resolve a peer host from AgentCard indexing (hostRegistry).
// MCP|WEB_SEARCH|SCRIPT|NEED_HUMAN_INPUT park *this* run (WaitingFor*).
// Returns parked=true when this run's status was set (do not mark Completed).
func applyNextAction(ctx context.Context, encryptKey *encrypt.EncryptKey, item *Item, llmText string) (parked bool, err error) {
	act, ok := ParseNextAction(llmText)
	if !ok || store == nil {
		return false, nil
	}

	if childType, ok := act.ChildActionType(); ok {
		childText := act.Payload
		if prompt := CapabilityPrompt(ctx, childType, act.Name); prompt != "" {
			childText = prompt + "\n\nINPUT:\n" + act.Payload
		}
		encPayload, err := encryptKey.Encrypt(childText)
		if err != nil {
			return false, fmt.Errorf("encrypt child payload: %w", err)
		}
		status := types.Pending
		if !LocalHasCapability(ctx, childType, act.Name) {
			status = ExternalWaitStatus(childType)
		}
		child := storage.ActionRun{
			SessionID:  item.sessionID,
			ActionName: act.Name,
			ActionType: childType,
			Payload:    encPayload,
			Status:     status,
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}
		if err := store.InsertActionRun(ctx, child); err != nil {
			return false, fmt.Errorf("enqueue %s/%s: %w", childType, act.Name, err)
		}
		executeLogger.Info(
			"enqueued next action",
			"session_id", item.sessionID,
			"parent", item.actionName,
			"child", act.Name,
			"type", string(childType),
			"status", string(status),
		)
		return false, nil
	}

	switch act.Kind {
	case NextHumanInput:
		msg := PublicFacingText(llmText)
		if msg == "" {
			msg = act.Payload
			if act.Name != "" {
				msg = act.Name + ": " + act.Payload
			}
		}
		return true, store.UpdateActionRunStatus(
			ctx, item.sessionID, item.actionName, item.actionType,
			types.WaitingForHumanInput, item.payload, encryptStoreResult(encryptKey, msg),
		)

	case NextMCP:
		_ = store.UpdateActionRunStatus(
			ctx, item.sessionID, item.actionName, item.actionType,
			types.WaitingForMCPAction, item.payload, encryptStoreResult(encryptKey, act.Name),
		)
		args := json.RawMessage(act.Payload)
		if !json.Valid(args) {
			args = mustJSONObject("raw", act.Payload)
		}
		out, err := CallMCPTool(ctx, act.Name, args)
		if err != nil {
			_ = store.UpdateActionRunStatus(
				ctx, item.sessionID, item.actionName, item.actionType,
				types.Failed, item.payload, encryptStoreResult(encryptKey, err.Error()),
			)
			return true, nil
		}
		if out.NeedHuman || (out.Code != "" && out.Result == "") {
			msg := out.Message
			if msg == "" {
				msg = "MCP tool missing or blocked; human input required"
			}
			_ = store.UpdateActionRunStatus(
				ctx, item.sessionID, item.actionName, item.actionType,
				types.WaitingForHumanInput, item.payload, encryptStoreResult(encryptKey, msg),
			)
			return true, nil
		}
		return true, resumeAfterSideEffect(ctx, encryptKey, item, act, out.Result)

	case NextWebSearch:
		_ = store.UpdateActionRunStatus(
			ctx, item.sessionID, item.actionName, item.actionType,
			types.WaitingForWebSearch, item.payload, encryptStoreResult(encryptKey, act.Name),
		)
		req, err := parseSearchRequest(act.Payload)
		if err != nil {
			_ = store.UpdateActionRunStatus(
				ctx, item.sessionID, item.actionName, item.actionType,
				types.Failed, item.payload, encryptStoreResult(encryptKey, err.Error()),
			)
			return true, nil
		}
		out := CallSearchBroker(ctx, req.Query)
		result := searchResultJSON(out)
		if out.Unavailable {
			_ = store.UpdateActionRunStatus(
				ctx, item.sessionID, item.actionName, item.actionType,
				types.Failed, item.payload, encryptStoreResult(encryptKey, result),
			)
			return true, nil
		}
		return true, resumeAfterSideEffect(ctx, encryptKey, item, act, result)

	case NextScript:
		_ = store.UpdateActionRunStatus(
			ctx, item.sessionID, item.actionName, item.actionType,
			types.WaitingForScript, item.payload, encryptStoreResult(encryptKey, act.Name),
		)
		req, err := parseScriptRequest(act.Payload)
		if err != nil {
			_ = store.UpdateActionRunStatus(
				ctx, item.sessionID, item.actionName, item.actionType,
				types.Failed, item.payload, encryptStoreResult(encryptKey, err.Error()),
			)
			return true, nil
		}
		res := RunScript(ctx, req)
		result := scriptResultJSON(res)
		if res.LanguageUnavailable || res.ExitCode != 0 {
			_ = store.UpdateActionRunStatus(
				ctx, item.sessionID, item.actionName, item.actionType,
				types.Failed, item.payload, encryptStoreResult(encryptKey, result),
			)
			return true, nil
		}
		return true, resumeAfterSideEffect(ctx, encryptKey, item, act, result)
	}

	return false, nil
}

// LocalHasCapability is true when any local workflow_manifest advertises name+type
// (same set exposed on this node's AgentCard). Missing → peer lookup via WaitingForExternal*.
func LocalHasCapability(ctx context.Context, actionType types.ActionType, name string) bool {
	return findCapability(ctx, actionType, name) != nil
}

// CapabilityPrompt returns the unit body from capabilities (data_source, else description).
// Used when enqueueing a local SKILL|SUB_AGENT|AGENT child so the worker sees the
// packaged markdown, not only the parent's ACTION JSON.
func CapabilityPrompt(ctx context.Context, actionType types.ActionType, name string) string {
	c := findCapability(ctx, actionType, name)
	if c == nil {
		return ""
	}
	if ds := strings.TrimSpace(c.GetDataSource()); ds != "" {
		return ds
	}
	return strings.TrimSpace(c.GetDescription())
}

func findCapability(ctx context.Context, actionType types.ActionType, name string) *manifest.TargetDescriptor {
	if store == nil || name == "" {
		return nil
	}
	manifests, err := store.GetAllWorkflowManifests(ctx)
	if err != nil {
		executeLogger.Error("list workflow manifests for capability check", "error", err)
		return nil
	}
	want := manifestTargetType(actionType)
	for _, m := range manifests {
		caps := make([]*manifest.TargetDescriptor, 0)
		if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
			continue
		}
		for _, c := range caps {
			if c == nil || c.GetName() != name {
				continue
			}
			if c.GetType() == want {
				return c
			}
		}
	}
	return nil
}

func manifestTargetType(actionType types.ActionType) manifest.TargetType {
	switch actionType {
	case types.Skill:
		return manifest.TargetType_SKILL
	case types.SubAgent:
		return manifest.TargetType_SUB_AGENT
	default:
		return manifest.TargetType_AGENT
	}
}

// ExternalWaitStatus is the WaitingForExternal* status for a child ActionType.
func ExternalWaitStatus(actionType types.ActionType) types.ActionRunStatus {
	switch actionType {
	case types.Skill:
		return types.WaitingForExternalSkillAction
	case types.SubAgent:
		return types.WaitingForExternalSubAgentAction
	default:
		return types.WaitingForExternalAgentAction
	}
}

// resumeAfterSideEffect stores the tool/script/search outcome in Result (encrypted)
// and requeues as Pending. On the next fetch, Result is moved into Payload.
func resumeAfterSideEffect(ctx context.Context, encryptKey *encrypt.EncryptKey, item *Item, act NextAction, result string) error {
	return store.UpdateActionRunStatus(
		ctx, item.sessionID, item.actionName, item.actionType,
		types.Pending, item.payload, encryptStoreResult(encryptKey, result),
	)
}

// encryptStoreResult encrypts plaintext for SQLite Result/parked fields.
// On encrypt failure (or nil key), stores PublicFacingText plaintext so the run can still park.
func encryptStoreResult(key *encrypt.EncryptKey, plain string) string {
	plain = PublicFacingText(plain)
	if key == nil || plain == "" {
		return plain
	}
	enc, err := key.Encrypt(plain)
	if err != nil {
		executeLogger.Error("encrypt store result failed; storing plaintext", "error", err)
		return plain
	}
	return enc
}
