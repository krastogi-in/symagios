package searchgateway

// ToolName is the custom tool every provider must declare instead of built-in search.
const ToolName = "web_search"

// AnthropicTool returns the custom tool schema for the Messages API.
// Do not include Anthropic's built-in web_search tool type.
func AnthropicTool() map[string]any {
	return map[string]any{
		"name":        ToolName,
		"description": "Search the web for current information via Symagios's governed search gateway.",
		"input_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query",
				},
			},
			"required": []string{"query"},
		},
	}
}

// OpenAITool returns the custom function tool schema for the Responses API.
// Do not include {"type": "web_search"}.
func OpenAITool() map[string]any {
	return map[string]any{
		"type":        "function",
		"name":        ToolName,
		"description": "Search the web via Symagios's governed search gateway.",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
			"required": []string{"query"},
		},
	}
}

// OpenAIChatTool returns the Chat Completions tools[] entry (not Responses API).
// Do not include {"type": "web_search"}.
func OpenAIChatTool() map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        ToolName,
			"description": "Search the web via Symagios's governed search gateway.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "The search query"},
				},
				"required": []string{"query"},
			},
		},
	}
}

// GeminiTool returns the function declaration for Gemini.
// Do not pass tools=[{"google_search": {}}].
func GeminiTool() map[string]any {
	return map[string]any{
		"name":        ToolName,
		"description": "Search the web via Symagios's governed search gateway.",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
			"required": []string{"query"},
		},
	}
}
