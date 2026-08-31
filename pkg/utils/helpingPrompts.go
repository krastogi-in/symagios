package utils

const (

	NextActionPrompt = `You have no provider web search. Do not call Anthropic, OpenAI, or Gemini built-in search.
	When you need the web, a skill, a subagent, a script, mcp, or a human, output exactly one action in this format:
	KIND::action-name{
	"key": "value"
	}
	Rules:
	- KIND must be one of: AGENT, SKILL, SUB_AGENT, SCRIPT, NEED_HUMAN_INPUT, MCP, WEB_SEARCH.
	- AGENT, SKILL, and SUB_AGENT start a child run of that ActionType.
	- SCRIPT, MCP, WEB_SEARCH, and NEED_HUMAN_INPUT park *this* agent (WaitingFor*) — they are not ActionTypes.
	- For SCRIPT, JSON must include "language" and "code" (e.g. {"language":"python3","code":"print(1)"}). If the runtime is missing the engine fails with "language not available".
	- For web search, output only this JSON shape:
	  WEB_SEARCH::web_search{"query":"cve-2024-1234 mitigations"}
	  The engine sends that query to Tavily while this run waits, then resumes with the result.
	- The name must contain only letters, digits, hyphens, or underscores.
	- The content inside braces must be valid JSON.
	- Do NOT wrap the command in markdown code blocks.
	- Output exactly one action per response.`

   CheckpointSummaryPrompt = `You need to give a summary of the execution of an in-progress task when we resume the execution.
   You are given the following information:
   - The target objective
   - The overall intent
   - The completed steps
   - The pending / remaining steps
   - The working memory summary
   You need to output the summary in the following format:
   SUMMARY::summary
   Rules:
   - SUMMARY must be a valid JSON object with the following fields:
   - target_objective
   - overall_intent
   - completed_steps: a list of completed steps
   - pending_steps: a list of pending / remaining steps
   - working_memory_summary: a summary of the working memory`

   ContinueTaskPrompt = `You need to continue the execution of an in-progress task. Do not repeat the completed steps. Resume execution directly from the next pending step.`
)