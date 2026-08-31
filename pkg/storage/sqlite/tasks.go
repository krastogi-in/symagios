package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"symagios/proto/a2a"
)

const upsertTaskSQL = `
INSERT INTO tasks (
	id, context_id, host, status, artifacts, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	context_id = excluded.context_id,
	host = excluded.host,
	status = excluded.status,
	artifacts = excluded.artifacts,
	updated_at = excluded.updated_at;
`

// InsertTask persists an A2A task and its directed host.
func (c *Client) InsertTask(ctx context.Context, task *a2a.Task, host string) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}

	now := time.Now().UTC()
	artifactsJSON, err := json.Marshal(task.Artifacts)
	if err != nil {
		return fmt.Errorf("marshal task artifacts: %w", err)
	}

	_, err = c.db.ExecContext(
		ctx,
		upsertTaskSQL,
		task.Id,
		task.ContextId,
		host,
		int32(task.Status),
		string(artifactsJSON),
		formatTime(now),
		formatTime(now),
	)
	if err != nil {
		return fmt.Errorf("insert task %q: %w", task.Id, err)
	}
	return nil
}

// GetTask returns a task and its host by ID.
func (c *Client) GetTask(ctx context.Context, id string) (*a2a.Task, string, error) {
	const query = `
SELECT id, context_id, host, status, artifacts
FROM tasks
WHERE id = ?;
`

	var (
		task          a2a.Task
		host          string
		status        int32
		artifactsJSON string
	)

	err := c.db.QueryRowContext(ctx, query, id).Scan(
		&task.Id,
		&task.ContextId,
		&host,
		&status,
		&artifactsJSON,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, "", fmt.Errorf("task %q not found", id)
		}
		return nil, "", fmt.Errorf("get task %q: %w", id, err)
	}

	task.Status = a2a.TaskState(status)
	if artifactsJSON != "" && artifactsJSON != "null" {
		if err := json.Unmarshal([]byte(artifactsJSON), &task.Artifacts); err != nil {
			return nil, "", fmt.Errorf("decode task %q artifacts: %w", id, err)
		}
	}
	return &task, host, nil
}

// ListTasksByContextID returns all tasks and hosts for a context/session.
func (c *Client) ListTasksByContextID(ctx context.Context, contextID string) ([]*a2a.Task, []string, error) {
	const query = `
SELECT id, context_id, host, status, artifacts
FROM tasks
WHERE context_id = ?
ORDER BY created_at ASC;
`

	rows, err := c.db.QueryContext(ctx, query, contextID)
	if err != nil {
		return nil, nil, fmt.Errorf("list tasks for context %q: %w", contextID, err)
	}
	defer rows.Close()

	var tasks []*a2a.Task
	var hosts []string
	for rows.Next() {
		var (
			task          a2a.Task
			host          string
			status        int32
			artifactsJSON string
		)
		if err := rows.Scan(&task.Id, &task.ContextId, &host, &status, &artifactsJSON); err != nil {
			return nil, nil, fmt.Errorf("scan task row: %w", err)
		}
		task.Status = a2a.TaskState(status)
		if artifactsJSON != "" && artifactsJSON != "null" {
			if err := json.Unmarshal([]byte(artifactsJSON), &task.Artifacts); err != nil {
				return nil, nil, fmt.Errorf("decode task artifacts: %w", err)
			}
		}
		tasks = append(tasks, &task)
		hosts = append(hosts, host)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate tasks for context %q: %w", contextID, err)
	}
	return tasks, hosts, nil
}

// UpdateTaskStatus updates the status of an existing task.
func (c *Client) UpdateTaskStatus(ctx context.Context, id string, status a2a.TaskState, statusMessage string) error {
	const query = `
UPDATE tasks
SET status = ?, updated_at = ?
WHERE id = ?;
`

	res, err := c.db.ExecContext(ctx, query, int32(status), formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("update task %q status: %w", id, err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update task %q rows affected: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("task %q not found", id)
	}
	return nil
}

// DeleteTask removes a task by ID. Missing tasks are ignored.
func (c *Client) DeleteTask(ctx context.Context, id string) error {
	const query = `DELETE FROM tasks WHERE id = ?;`

	_, err := c.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete task %q: %w", id, err)
	}
	return nil
}

// CancelTask marks a task as canceled.
func (c *Client) CancelTask(ctx context.Context, id string) error {
	return c.UpdateTaskStatus(ctx, id, a2a.TaskState_TASK_STATE_CANCELED, "")
}
