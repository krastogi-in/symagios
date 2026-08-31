package agentgateway

import (
	"fmt"
	"strings"

	"symagios/proto/a2a"
	"symagios/utils/types"
)

func onServerTaskID(sessionID, actionName string, actionType types.ActionType) string {
	return fmt.Sprintf("%s:%s:%s", sessionID, actionName, actionType)
}

func parseOnServerTaskID(taskID string) (sessionID, actionName string, actionType types.ActionType, err error) {
	parts := strings.SplitN(taskID, ":", 3)
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("invalid on-server task id %q", taskID)
	}
	return parts[0], parts[1], types.ActionType(parts[2]), nil
}

func actionStatusToTaskStatus(status types.ActionRunStatus) a2a.TaskState {
	switch status {
	case types.Pending:
		return a2a.TaskState_TASK_STATE_SUBMITTED
	case types.Running:
		return a2a.TaskState_TASK_STATE_WORKING
	case types.WaitingForHumanInput,
		types.WaitingForMCPAction,
		types.WaitingForWebSearch,
		types.WaitingForScript,
		types.WaitingForExternalAgentAction,
		types.WaitingForExternalSkillAction,
		types.WaitingForExternalSubAgentAction:
		return a2a.TaskState_TASK_STATE_INPUT_REQUIRED
	case types.Completed:
		return a2a.TaskState_TASK_STATE_COMPLETED
	case types.Failed:
		return a2a.TaskState_TASK_STATE_FAILED
	default:
		return a2a.TaskState_TASK_STATE_UNSPECIFIED
	}
}

func isTerminalState(state a2a.TaskState) bool {
	switch state {
	case a2a.TaskState_TASK_STATE_COMPLETED,
		a2a.TaskState_TASK_STATE_FAILED,
		a2a.TaskState_TASK_STATE_CANCELED:
		return true
	default:
		return false
	}
}
