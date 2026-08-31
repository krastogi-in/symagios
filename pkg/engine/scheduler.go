package engine

import (
	"context"
	"time"

	"symagios/encrypt"
	applog "symagios/log"
	"symagios/utils"
	"symagios/utils/types"
)

const pollingInterval = 10 * time.Second

var schedulerLogger = applog.Default().With("component", "scheduler")

func startScheduler(ctx context.Context, buffer *utils.Buffer) {
	go func() {
		for {
			if err := promoteLocalExternalWaits(ctx); err != nil {
				schedulerLogger.Error("failed to promote local external waits", "error", err)
			}
			if err := runPendingActionRuns(ctx, buffer); err != nil {
				schedulerLogger.Error("failed to run pending action runs", "error", err)
			}
			time.Sleep(pollingInterval)
		}
	}()
}

// PromoteLocalExternalWaits flips WaitingForExternal* → Pending when the capability
// now exists on this worker group (shared SQLite / local manifests) — e.g. another
// agent was uploaded after the child was parked. Avoids needless peer A2A.
func PromoteLocalExternalWaits(ctx context.Context) error {
	return promoteLocalExternalWaits(ctx)
}

func promoteLocalExternalWaits(ctx context.Context) error {
	if store == nil {
		return nil
	}
	for _, status := range []types.ActionRunStatus{
		types.WaitingForExternalAgentAction,
		types.WaitingForExternalSkillAction,
		types.WaitingForExternalSubAgentAction,
	} {
		runs, err := store.ListActionRunsByStatus(ctx, status)
		if err != nil {
			return err
		}
		for _, run := range runs {
			if !LocalHasCapability(ctx, run.ActionType, run.ActionName) {
				continue
			}
			if err := store.UpdateActionRunStatus(
				ctx, run.SessionID, run.ActionName, run.ActionType,
				types.Pending, run.Payload, run.Result,
			); err != nil {
				return err
			}
			schedulerLogger.Info(
				"promoted external wait to local pending (capability now on this worker group)",
				applog.ActionFields(run.SessionID, run.ActionName, run.ActionType)...,
			)
		}
	}
	return nil
}

func runPendingActionRuns(ctx context.Context, buffer *utils.Buffer) error {
	runs, err := store.ListActionRunsByStatus(ctx, types.Pending)
	if err != nil {
		return err
	}

	for _, run := range runs {
		folded, cleared, changed, err := encrypt.FoldResultIntoPayload(storeEncrypt, run.Payload, run.Result)
		if err != nil {
			return err
		}
		if changed {
			run.Payload, run.Result = folded, cleared
			if err := store.UpdateActionRunStatus(
				ctx, run.SessionID, run.ActionName, run.ActionType,
				run.Status, run.Payload, run.Result,
			); err != nil {
				return err
			}
		}
		claimed, err := store.ClaimPendingActionRun(ctx, run.SessionID, run.ActionName, run.ActionType)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}

		item := &Item{
			sessionID:  run.SessionID,
			actionName: run.ActionName,
			actionType: run.ActionType,
			payload:    run.Payload,
			status:     types.Running,
		}
		if !buffer.Add(item) {
			// Buffer full — release claim so the next poll can retry.
			_ = store.UpdateActionRunStatus(
				ctx, run.SessionID, run.ActionName, run.ActionType,
				types.Pending, run.Payload, run.Result,
			)
			schedulerLogger.Debug(
				"buffer full; released pending claim",
				applog.ActionFields(run.SessionID, run.ActionName, run.ActionType)...,
			)
			continue
		}

		schedulerLogger.Debug(
			"claimed and queued pending action run",
			applog.ActionFields(run.SessionID, run.ActionName, run.ActionType)...,
		)
	}
	return nil
}
