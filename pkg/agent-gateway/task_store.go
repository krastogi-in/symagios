package agentgateway

import (
	"context"

	"symagios/proto/a2a"
	"symagios/storage/sqlite"
	"symagios/utils"
)

type taskStore struct {
	db    *sqlite.Client
	cache *utils.LRUCache[*TaskRecord]
}

func newTaskStore(db *sqlite.Client, cache *utils.LRUCache[*TaskRecord]) *taskStore {
	return &taskStore{db: db, cache: cache}
}

func (s *taskStore) Save(ctx context.Context, record *TaskRecord) error {
	if record == nil || record.Task == nil {
		return nil
	}
	if err := s.db.InsertTask(ctx, record.Task, record.Host); err != nil {
		return err
	}
	s.cache.Add(record.Task.Id, record)
	return nil
}

func (s *taskStore) Get(ctx context.Context, id string) (*TaskRecord, error) {
	if record, ok := s.cache.Get(id); ok {
		return record, nil
	}

	task, host, err := s.db.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	record := NewTaskRecord(task, host, 0)
	s.cache.Add(id, record)
	return record, nil
}

func (s *taskStore) ListByContext(ctx context.Context, contextID string) ([]*TaskRecord, error) {
	tasks, hosts, err := s.db.ListTasksByContextID(ctx, contextID)
	if err != nil {
		return nil, err
	}

	active := make([]*TaskRecord, 0, len(tasks))
	for i, task := range tasks {
		record := NewTaskRecord(task, hosts[i], 0)
		s.cache.Add(task.Id, record)
		active = append(active, record)
	}
	return active, nil
}

func (s *taskStore) UpdateStatus(ctx context.Context, id string, status a2a.TaskState, statusMessage string) error {
	if isTerminalState(status) {
		return s.Remove(ctx, id)
	}
	if err := s.db.UpdateTaskStatus(ctx, id, status, statusMessage); err != nil {
		return err
	}
	task, host, err := s.db.GetTask(ctx, id)
	if err != nil {
		return err
	}
	s.cache.Add(id, NewTaskRecord(task, host, 0))
	return nil
}

func (s *taskStore) Cancel(ctx context.Context, id string) error {
	return s.Remove(ctx, id)
}

func (s *taskStore) Remove(ctx context.Context, id string) error {
	s.cache.Remove(id)
	return s.db.DeleteTask(ctx, id)
}

func (s *taskStore) PurgeTerminalForContext(ctx context.Context, contextID string) error {
	tasks, _, err := s.db.ListTasksByContextID(ctx, contextID)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if isTerminalState(task.Status) {
			_ = s.Remove(ctx, task.Id)
		}
	}
	return nil
}
