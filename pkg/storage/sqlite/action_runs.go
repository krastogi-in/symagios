package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"symagios/storage"
	"symagios/utils/types"
)

const upsertActionRunSQL = `
INSERT INTO action_runs (
	session_id, action_name, action_type, status, payload, result, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id, action_name, action_type) DO UPDATE SET
	status = excluded.status,
	payload = excluded.payload,
	result = excluded.result,
	updated_at = excluded.updated_at;
`

const upsertWorkflowManifestSQL = `
INSERT INTO workflow_manifest (
	session_id, capabilities, workflow_steps, created_at, updated_at
) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
	capabilities = excluded.capabilities,
	workflow_steps = excluded.workflow_steps,
	updated_at = excluded.updated_at;
`

func (c *Client) InsertActionRun(ctx context.Context, run storage.ActionRun) error {
	now := time.Now().UTC()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = now
	}

	_, err := c.db.ExecContext(
		ctx,
		upsertActionRunSQL,
		run.SessionID,
		run.ActionName,
		string(run.ActionType),
		string(run.Status),
		run.Payload,
		run.Result,
		formatTime(run.CreatedAt),
		formatTime(run.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert action run %q/%q: %w", run.SessionID, run.ActionName, err)
	}
	return nil
}

func (c *Client) UpdateActionRunStatus(ctx context.Context, sessionID, actionName string, actionType types.ActionType, status types.ActionRunStatus, payload string, result string) error {
	now := formatTime(time.Now().UTC())

	const query = `
UPDATE action_runs
SET status = ?, payload = ?, result = ?, updated_at = ?
WHERE session_id = ? AND action_name = ? AND action_type = ?;
`

	res, err := c.db.ExecContext(ctx, query, string(status), payload, result, now, sessionID, actionName, string(actionType))
	if err != nil {
		return fmt.Errorf("update action run %q/%q: %w", sessionID, actionName, err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update action run rows affected %q/%q: %w", sessionID, actionName, err)
	}
	if rows == 0 {
		return fmt.Errorf("action run %q/%q not found", sessionID, actionName)
	}
	return nil
}

// ClaimPendingActionRun sets status to RUNNING only if currently PENDING.
func (c *Client) ClaimPendingActionRun(ctx context.Context, sessionID, actionName string, actionType types.ActionType) (bool, error) {
	now := formatTime(time.Now().UTC())
	const query = `
UPDATE action_runs
SET status = ?, updated_at = ?
WHERE session_id = ? AND action_name = ? AND action_type = ? AND status = ?;
`
	res, err := c.db.ExecContext(
		ctx, query,
		string(types.Running), now,
		sessionID, actionName, string(actionType), string(types.Pending),
	)
	if err != nil {
		return false, fmt.Errorf("claim action run %q/%q: %w", sessionID, actionName, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim action run rows affected %q/%q: %w", sessionID, actionName, err)
	}
	return rows == 1, nil
}

func (c *Client) GetActionRun(ctx context.Context, sessionID, actionName string, actionType types.ActionType) (storage.ActionRun, error) {
	const query = `
SELECT session_id, action_name, action_type, status, payload, result, created_at, updated_at
FROM action_runs
WHERE session_id = ? AND action_name = ? AND action_type = ?;
`

	row := c.db.QueryRowContext(ctx, query, sessionID, actionName, string(actionType))
	run, err := scanActionRunRow(row)
	if err != nil {
		return storage.ActionRun{}, err
	}
	return run, nil
}

func (c *Client) ListActiveActionRuns(ctx context.Context, sessionID string) ([]storage.ActionRun, error) {
	const query = `
SELECT session_id, action_name, action_type, status, payload, result, created_at, updated_at
FROM action_runs
WHERE session_id = ?
	AND status NOT IN (?, ?)
ORDER BY created_at ASC;
`

	rows, err := c.db.QueryContext(
		ctx,
		query,
		sessionID,
		types.Completed,
		types.Failed,
	)
	if err != nil {
		return nil, fmt.Errorf("list active action runs for session %q: %w", sessionID, err)
	}
	defer rows.Close()

	return scanActionRuns(rows)
}

func (c *Client) ListActionRunsBySession(ctx context.Context, sessionID string) ([]storage.ActionRun, error) {
	const query = `
SELECT session_id, action_name, action_type, status, payload, result, created_at, updated_at
FROM action_runs
WHERE session_id = ?
ORDER BY created_at ASC;
`

	rows, err := c.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list action runs for session %q: %w", sessionID, err)
	}
	defer rows.Close()

	return scanActionRuns(rows)
}

func (c *Client) ListActionRunsByStatus(ctx context.Context, status types.ActionRunStatus) ([]storage.ActionRun, error) {
	const query = `
SELECT session_id, action_name, action_type, status, payload, result, created_at, updated_at
FROM action_runs
WHERE status = ?
ORDER BY updated_at ASC;
`

	rows, err := c.db.QueryContext(ctx, query, string(status))
	if err != nil {
		return nil, fmt.Errorf("list action runs by status %q: %w", status, err)
	}
	defer rows.Close()

	return scanActionRuns(rows)
}

func (c *Client) ListAllActionRuns(ctx context.Context) ([]storage.ActionRun, error) {
	const query = `
SELECT session_id, action_name, action_type, status, payload, result, created_at, updated_at
FROM action_runs
ORDER BY updated_at ASC, created_at ASC;
`

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list all action runs: %w", err)
	}
	defer rows.Close()

	return scanActionRuns(rows)
}

func (c *Client) PurgeCompletedActionRunsBefore(ctx context.Context, before time.Time) (int64, error) {
	const query = `
DELETE FROM action_runs
WHERE status = ? AND updated_at < ?;
`

	res, err := c.db.ExecContext(
		ctx,
		query,
		types.Completed,
		formatTime(before.UTC()),
	)
	if err != nil {
		return 0, fmt.Errorf("purge completed action runs: %w", err)
	}

	return res.RowsAffected()
}

func (c *Client) InsertWorkflowManifest(ctx context.Context, manifest storage.WorkflowManifest) error {
	now := time.Now().UTC()
	if manifest.CreatedAt.IsZero() {
		manifest.CreatedAt = now
	}
	if manifest.UpdatedAt.IsZero() {
		manifest.UpdatedAt = now
	}

	_, err := c.db.ExecContext(
		ctx,
		upsertWorkflowManifestSQL,
		manifest.SessionID,
		manifest.Capabilities,
		manifest.WorkflowSteps,
		formatTime(manifest.CreatedAt),
		formatTime(manifest.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert workflow manifest %q: %w", manifest.SessionID, err)
	}
	return nil
}

func (c *Client) GetWorkflowManifest(ctx context.Context, sessionID string) (storage.WorkflowManifest, error) {
	const query = `
SELECT session_id, capabilities, workflow_steps, created_at, updated_at
FROM workflow_manifest
WHERE session_id = ?;
`

	row := c.db.QueryRowContext(ctx, query, sessionID)
	return scanWorkflowManifestRow(row)
}

func (c *Client) GetAllWorkflowManifests(ctx context.Context) ([]storage.WorkflowManifest, error) {
	const query = `
SELECT session_id, capabilities, workflow_steps, created_at, updated_at
FROM workflow_manifest;
`

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get all workflow manifests: %w", err)
	}
	defer rows.Close()

	return scanWorkflowManifests(rows)
}

func (c *Client) PurgeWorkflowManifestsBefore(ctx context.Context, before time.Time) (int64, error) {
	const query = `
DELETE FROM workflow_manifest
WHERE updated_at < ?;
`

	res, err := c.db.ExecContext(ctx, query, formatTime(before.UTC()))
	if err != nil {
		return 0, fmt.Errorf("purge workflow manifests: %w", err)
	}

	return res.RowsAffected()
}

func scanActionRunRow(row *sql.Row) (storage.ActionRun, error) {
	var run storage.ActionRun
	var actionType string
	var status string
	var createdAt string
	var updatedAt string

	err := row.Scan(
		&run.SessionID,
		&run.ActionName,
		&actionType,
		&status,
		&run.Payload,
		&run.Result,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return storage.ActionRun{}, fmt.Errorf("scan action run: %w", err)
	}

	run.ActionType = types.ActionType(actionType)
	run.Status = types.ActionRunStatus(status)
	run.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return storage.ActionRun{}, err
	}
	run.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return storage.ActionRun{}, err
	}

	return run, nil
}

func scanActionRuns(rows *sql.Rows) ([]storage.ActionRun, error) {
	runs := make([]storage.ActionRun, 0)
	for rows.Next() {
		var run storage.ActionRun
		var actionType string
		var status string
		var createdAt string
		var updatedAt string

		if err := rows.Scan(
			&run.SessionID,
			&run.ActionName,
			&actionType,
			&status,
			&run.Payload,
			&run.Result,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan action run row: %w", err)
		}

		run.ActionType = types.ActionType(actionType)
		run.Status = types.ActionRunStatus(status)
		var err error
		run.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		run.UpdatedAt, err = parseTime(updatedAt)
		if err != nil {
			return nil, err
		}

		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate action runs: %w", err)
	}
	return runs, nil
}

func scanWorkflowManifestRow(row *sql.Row) (storage.WorkflowManifest, error) {
	var manifest storage.WorkflowManifest
	var createdAt string
	var updatedAt string

	if err := row.Scan(
		&manifest.SessionID,
		&manifest.Capabilities,
		&manifest.WorkflowSteps,
		&createdAt,
		&updatedAt,
	); err != nil {
		return storage.WorkflowManifest{}, fmt.Errorf("scan workflow manifest: %w", err)
	}

	var err error
	manifest.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return storage.WorkflowManifest{}, err
	}
	manifest.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return storage.WorkflowManifest{}, err
	}

	return manifest, nil
}

func scanWorkflowManifests(rows *sql.Rows) ([]storage.WorkflowManifest, error) {
	manifests := make([]storage.WorkflowManifest, 0)
	for rows.Next() {
		var manifest storage.WorkflowManifest
		var createdAt string
		var updatedAt string

		if err := rows.Scan(
			&manifest.SessionID,
			&manifest.Capabilities,
			&manifest.WorkflowSteps,
			&createdAt,
			&updatedAt,
		); err != nil {
			continue
		}

		var err error
		manifest.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			continue
		}
		manifest.UpdatedAt, err = parseTime(updatedAt)
		if err != nil {
			continue
		}

		manifests = append(manifests, manifest)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workflow manifests: %w", err)
	}
	return manifests, nil
}
