package agentgateway

import (
	"context"
	"time"

	"symagios/engine"
	"symagios/storage"
)

func (g *AgentGateway) syncOnServerTaskFromID(ctx context.Context, taskID string) error {
	sessionID, actionName, actionType, err := parseOnServerTaskID(taskID)
	if err != nil {
		return err
	}

	run, err := g.db.GetActionRun(ctx, sessionID, actionName, actionType)
	if err != nil {
		return nil
	}
	// Do not FoldResultIntoPayload here — scheduler owns fold-on-claim (avoids duplicate history).
	return g.syncOnServerTaskFromActionRun(ctx, run)
}

func (g *AgentGateway) syncOnServerTaskFromActionRun(ctx context.Context, run storage.ActionRun) error {
	taskID := onServerTaskID(run.SessionID, run.ActionName, run.ActionType)

	if _, err := g.onServerTasks.Get(ctx, taskID); err != nil {
		return nil
	}

	msg := publicResult(g, run.Payload)
	if msg == "" {
		msg = publicResult(g, run.Result)
	}
	return g.onServerTasks.UpdateStatus(ctx, taskID, actionStatusToTaskStatus(run.Status), msg)
}

func publicResult(g *AgentGateway, raw string) string {
	if raw == "" {
		return ""
	}
	text := raw
	if g.encryptKey != nil {
		text = g.encryptKey.DecryptStored(raw)
	}
	return engine.PublicFacingText(text)
}

func (g *AgentGateway) syncOnServerTasksForSession(ctx context.Context, sessionID string) error {
	runs, err := g.db.ListActionRunsBySession(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, run := range runs {
		if err := g.syncOnServerTaskFromActionRun(ctx, run); err != nil {
			g.log.Error(
				"failed to sync on-server task from action run",
				"session_id", run.SessionID,
				"action_name", run.ActionName,
				"action_type", run.ActionType,
				"error", err,
			)
		}
	}
	return nil
}

func (g *AgentGateway) pollOnServerTasks(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			manifests, err := g.db.GetAllWorkflowManifests(ctx)
			if err != nil {
				g.log.Error("failed to list workflow manifests for on-server sync", "error", err)
				continue
			}
			for _, manifest := range manifests {
				if err := g.syncOnServerTasksForSession(ctx, manifest.SessionID); err != nil {
					g.log.Error("failed to sync on-server tasks", "session_id", manifest.SessionID, "error", err)
				}
			}
		}
	}
}
