package mcpgateway

import (
	"encoding/json"
)

// CallRequest is what the engine (or agent) sends to the gateway.
type CallRequest struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Source    string          `json:"source,omitempty"` // ignored on HTTP; set from client cert
}

// CallResponse is returned on success.
type CallResponse struct {
	Tool   string `json:"tool"`
	Server string `json:"server"`
	Result string `json:"result"`
}

// ErrorBody is a structured failure. NeedHuman means the engine should move
// the action run to WAITING_FOR_HUMAN_INPUT.
type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Tool      string `json:"tool,omitempty"`
	NeedHuman bool   `json:"need_human"`
}

const (
	CodeToolNotFound   = "tool_not_found"
	CodeToolNotAllowed = "tool_not_allowed"
	CodeContentBlocked = "content_blocked"
	CodeUpstream       = "upstream_error"
	CodeVault          = "vault_error"
)

func stubCall(tool string, args json.RawMessage) string {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	out, _ := json.Marshal(map[string]any{
		"stub":      true,
		"tool":      tool,
		"arguments": json.RawMessage(args),
		"message":   "mcp-gateway stub executed attached tool (no upstream)",
	})
	return string(out)
}

func rawOrEmptyObject(args json.RawMessage) json.RawMessage {
	if len(args) == 0 {
		return json.RawMessage(`{}`)
	}
	return args
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
