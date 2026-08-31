package types

type ActionRunStatus string

const (
	Pending                          ActionRunStatus = "PENDING"
	Running                          ActionRunStatus = "RUNNING"
	WaitingForHumanInput             ActionRunStatus = "WAITING_FOR_HUMAN_INPUT"
	WaitingForMCPAction              ActionRunStatus = "WAITING_FOR_MCP_ACTION"
	WaitingForWebSearch              ActionRunStatus = "WAITING_FOR_WEB_SEARCH"
	WaitingForScript                 ActionRunStatus = "WAITING_FOR_SCRIPT"
	WaitingForExternalAgentAction    ActionRunStatus = "WAITING_FOR_EXTERNAL_AGENT_ACTION"
	WaitingForExternalSkillAction    ActionRunStatus = "WAITING_FOR_EXTERNAL_SKILL_ACTION"
	WaitingForExternalSubAgentAction ActionRunStatus = "WAITING_FOR_EXTERNAL_SUB_AGENT_ACTION"
	Completed                        ActionRunStatus = "COMPLETED"
	Failed                           ActionRunStatus = "FAILED"
)

type ActionType string

const (
	Agent    ActionType = "AGENT"
	Skill    ActionType = "SKILL"
	SubAgent ActionType = "SUB_AGENT"
)
