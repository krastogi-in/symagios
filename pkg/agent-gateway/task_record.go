package agentgateway

import "symagios/proto/a2a"

// TaskRecord is a gateway task with its directed external host.
type TaskRecord struct {
	Task *a2a.Task
	Host string
	Attempts int
}

func NewTaskRecord(task *a2a.Task, host string, attempts int) *TaskRecord {
	return &TaskRecord{Task: task, Host: host, Attempts: attempts}
}