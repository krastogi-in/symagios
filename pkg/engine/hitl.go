package engine

import (
	"context"
	"fmt"

	"symagios/engine/hooks"
	"symagios/utils/types"
)

// HumanInputResponse is operator input for a paused action run.
type HumanInputResponse struct {
	SessionID  string
	ActionName string
	ActionType types.ActionType
	Input      string
}

// SubmitHumanInput stores the operator reply in Result (encrypted when a key is
// configured) and moves the run to Pending. On the next fetch, Result becomes Payload.
func SubmitHumanInput(ctx context.Context, resp HumanInputResponse) error {
	if store == nil {
		return fmt.Errorf("engine store not initialized; call InitStore from watch")
	}
	if resp.SessionID == "" || resp.ActionName == "" {
		return fmt.Errorf("session_id and action_name are required")
	}
	actionType := resp.ActionType
	if actionType == "" {
		actionType = types.Agent
	}

	input := PublicFacingText(resp.Input)
	pre, err := hooks.ProtectPreLLM(input)
	if err != nil {
		return fmt.Errorf("human input blocked by content guard: %w", err)
	}
	input = pre.Text

	run, err := store.GetActionRun(ctx, resp.SessionID, resp.ActionName, actionType)
	if err != nil {
		return fmt.Errorf("load action run: %w", err)
	}

	encResult := input
	if storeEncrypt != nil {
		enc, err := storeEncrypt.Encrypt(input)
		if err != nil {
			return fmt.Errorf("encrypt human input result: %w", err)
		}
		encResult = enc
	}

	return store.UpdateActionRunStatus(
		ctx,
		resp.SessionID,
		resp.ActionName,
		actionType,
		types.Pending,
		run.Payload,
		encResult,
	)
}
