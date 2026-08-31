package engine

import (
	"context"
	"encoding/json"

	"symagios/encrypt"
	"symagios/engine/hooks"
	applog "symagios/log"
	"symagios/utils"
	"symagios/utils/types"
)

var executeLogger = applog.Default().With("component", "execute")

func storeOutcome(ctx context.Context, encryptKey *encrypt.EncryptKey, item *Item, status types.ActionRunStatus, result string) {
	stored := encryptStoreResult(encryptKey, result)
	if err := store.UpdateActionRunStatus(
		ctx,
		item.sessionID,
		item.actionName,
		item.actionType,
		status,
		item.payload,
		stored,
	); err != nil {
		executeLogger.Error(
			"failed to update action run",
			append(applog.ActionFields(item.sessionID, item.actionName, item.actionType), "status", string(status), "error", err)...,
		)
	}
}

// parkForHuman parks the run so cli watch can surface the issue to an operator.
func parkForHuman(ctx context.Context, encryptKey *encrypt.EncryptKey, item *Item, reason string) {
	storeOutcome(ctx, encryptKey, item, types.WaitingForHumanInput, reason)
}

// execute runs Agent|Skill|SubAgent work.
// Flow: pre-LLM → eBPF authorize → LLM → post-LLM → applyNextAction → Completed
// (only if no wait status).
func execute(ctx context.Context, encryptKey *encrypt.EncryptKey, item *Item, cgroupID uint64) {
	if store == nil || encryptKey == nil || item == nil {
		return
	}

	if err := store.UpdateActionRunStatus(
		ctx,
		item.sessionID,
		item.actionName,
		item.actionType,
		types.Running,
		item.payload,
		"",
	); err != nil {
		executeLogger.Error(
			"failed to mark action as running",
			append(applog.ActionFields(item.sessionID, item.actionName, item.actionType), "error", err)...,
		)
		return
	}

	decryptedPayload := PublicFacingText(encryptKey.DecryptStored(item.payload))

	pre, err := hooks.ProtectPreLLM(decryptedPayload)
	if err != nil {
		executeLogger.Error(
			"pre-LLM content guard blocked payload",
			append(applog.ActionFields(item.sessionID, item.actionName, item.actionType), "error", err, "reason", pre.Reason)...,
		)
		parkForHuman(ctx, encryptKey, item, pre.Reason)
		return
	}
	decryptedPayload = pre.Text

	if err := hooks.AuthorizeWorker(cgroupID, item.actionType); err != nil {
		executeLogger.Error(
			"failed to authorize worker policy",
			append(applog.ActionFields(item.sessionID, item.actionName, item.actionType), "error", err)...,
		)
		_ = hooks.RevokeWorker(cgroupID)
		parkForHuman(ctx, encryptKey, item, err.Error())
		return
	}
	defer func() { _ = hooks.RevokeWorker(cgroupID) }()

	system := "You are a Symagios sandboxed " + string(item.actionType) + " worker. Follow system policy.\n\n" + utils.NextActionPrompt
	text, err := CompleteLLM(ctx, system, decryptedPayload)
	if err != nil {
		executeLogger.Info("llm skipped or failed; using payload as result",
			append(applog.ActionFields(item.sessionID, item.actionName, item.actionType), "error", err)...)
		item.result = decryptedPayload
	} else {
		item.result = text
	}

	post, err := hooks.ProtectPostLLM(item.result)
	if err != nil {
		executeLogger.Error(
			"post-LLM content guard blocked result",
			append(applog.ActionFields(item.sessionID, item.actionName, item.actionType), "error", err, "reason", post.Reason)...,
		)
		parkForHuman(ctx, encryptKey, item, post.Reason)
		return
	}
	item.result = post.Text

	// MCP / search / script / HITL park this run (WaitingFor*). Child AGENT|SKILL|SUB_AGENT
	// enqueue does not — parent still Completes below.
	parked, derr := applyNextAction(ctx, encryptKey, item, item.result)
	if derr != nil {
		executeLogger.Error("next action dispatch failed",
			append(applog.ActionFields(item.sessionID, item.actionName, item.actionType), "error", derr)...)
		parkForHuman(ctx, encryptKey, item, derr.Error())
		return
	}
	if parked {
		return
	}

	result, err := encryptKey.Encrypt(PublicFacingText(item.result))
	if err != nil {
		executeLogger.Error(
			"failed to encrypt result",
			append(applog.ActionFields(item.sessionID, item.actionName, item.actionType), "error", err)...,
		)
		parkForHuman(ctx, encryptKey, item, "encrypt result: "+err.Error())
		return
	}
	if err := store.UpdateActionRunStatus(
		ctx,
		item.sessionID,
		item.actionName,
		item.actionType,
		types.Completed,
		item.payload,
		result,
	); err != nil {
		executeLogger.Error(
			"failed to mark action as completed",
			append(applog.ActionFields(item.sessionID, item.actionName, item.actionType), "error", err)...,
		)
		parkForHuman(ctx, encryptKey, item, "complete update: "+err.Error())
	}
}

func mustJSONObject(key, value string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{key: value})
	return b
}
