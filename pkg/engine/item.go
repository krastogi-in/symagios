package engine

import (
	"symagios/utils/types"
)

// Item is a schedulable unit of work loaded from storage.
type Item struct {
	sessionID  string
	actionName string
	actionType types.ActionType
	payload    string
	result     string
	status     types.ActionRunStatus
}

func (i *Item) CanExecute() bool {
	// Scheduler claims Pending → Running before enqueue; workers accept both.
	return i != nil && (i.status == types.Pending || i.status == types.Running)
}
