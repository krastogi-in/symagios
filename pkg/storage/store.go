package storage

import (
	"context"
	"time"

	"symagios/utils/types"
)

// ActionRun is persisted execution state for one action within a session.
type ActionRun struct {
	SessionID  string
	ActionName string
	ActionType types.ActionType
	Status     types.ActionRunStatus
	Payload    string
	Result     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// WorkflowManifest is the static workflow definition for a session.
type WorkflowManifest struct {
	SessionID     string
	Capabilities  string
	WorkflowSteps string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Store is the persistence contract used by the engine.
type Store interface {
	Close() error

	InsertActionRun(ctx context.Context, run ActionRun) error
	UpdateActionRunStatus(ctx context.Context, sessionID, actionName string, actionType types.ActionType, status types.ActionRunStatus, payload string, result string) error
	// ClaimPendingActionRun atomically moves PENDING → RUNNING. Returns false if
	ClaimPendingActionRun(ctx context.Context, sessionID, actionName string, actionType types.ActionType) (bool, error)
	GetActionRun(ctx context.Context, sessionID, actionName string, actionType types.ActionType) (ActionRun, error)
	ListActiveActionRuns(ctx context.Context, sessionID string) ([]ActionRun, error)
	ListActionRunsBySession(ctx context.Context, sessionID string) ([]ActionRun, error)
	ListActionRunsByStatus(ctx context.Context, status types.ActionRunStatus) ([]ActionRun, error)
	ListAllActionRuns(ctx context.Context) ([]ActionRun, error)
	PurgeCompletedActionRunsBefore(ctx context.Context, before time.Time) (int64, error)

	InsertWorkflowManifest(ctx context.Context, manifest WorkflowManifest) error
	GetWorkflowManifest(ctx context.Context, sessionID string) (WorkflowManifest, error)
	GetAllWorkflowManifests(ctx context.Context) ([]WorkflowManifest, error)
	PurgeWorkflowManifestsBefore(ctx context.Context, before time.Time) (int64, error)
}
