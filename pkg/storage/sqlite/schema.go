package sqlite

const schemaSQL = `
CREATE TABLE IF NOT EXISTS action_runs (
	session_id TEXT NOT NULL,
	action_name TEXT NOT NULL,
	action_type TEXT NOT NULL,
	status TEXT NOT NULL,
	payload TEXT NOT NULL DEFAULT '',
	result TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (session_id, action_name, action_type)
);

CREATE INDEX IF NOT EXISTS idx_action_runs_session_status
	ON action_runs (session_id, status);

CREATE INDEX IF NOT EXISTS idx_action_runs_created_at
	ON action_runs (created_at);

CREATE TABLE IF NOT EXISTS workflow_manifest (
	session_id TEXT NOT NULL PRIMARY KEY,
	capabilities TEXT NOT NULL,
	workflow_steps TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
	id TEXT NOT NULL PRIMARY KEY,
	context_id TEXT NOT NULL,
	host TEXT NOT NULL,
	status TEXT NOT NULL,
	artifacts TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_context_status
	ON tasks (context_id, status);

`
