package agentgateway

import (
	"testing"

	"symagios/proto/a2a"
	"symagios/utils/types"
)

func TestOnServerTaskIDRoundTrip(t *testing.T) {
	id := onServerTaskID("sess", "act", types.Agent)
	s, a, typ, err := parseOnServerTaskID(id)
	if err != nil || s != "sess" || a != "act" || typ != types.Agent {
		t.Fatalf("%q %v %v %v %v", id, s, a, typ, err)
	}
}

func TestActionStatusToTaskStatus(t *testing.T) {
	if actionStatusToTaskStatus(types.WaitingForHumanInput) != a2a.TaskState_TASK_STATE_INPUT_REQUIRED {
		t.Fatal("HITL should map to input-required")
	}
	if actionStatusToTaskStatus(types.WaitingForMCPAction) != a2a.TaskState_TASK_STATE_INPUT_REQUIRED {
		t.Fatal("MCP wait should map to input-required")
	}
	if actionStatusToTaskStatus(types.Completed) != a2a.TaskState_TASK_STATE_COMPLETED {
		t.Fatal("completed")
	}
	if !isTerminalState(a2a.TaskState_TASK_STATE_FAILED) {
		t.Fatal("failed is terminal")
	}
}
