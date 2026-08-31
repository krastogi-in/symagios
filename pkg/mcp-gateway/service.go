package mcpgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"symagios/contentguard"
	"symagios/egress"
	"symagios/engine/hooks"
)

// Service is the secured MCP gateway: registry + Vault + content guards + upstream.
type Service struct {
	cfg      Config
	registry *Registry
	http     *http.Client
}

// New builds a gateway from config and a loaded registry.
func New(cfg Config, registry *Registry) *Service {
	if cfg.ContentGuards {
		hooks.ConfigureContentGuards(contentguard.DefaultConfig())
	}
	httpClient, err := egress.Config{
		Proxy:      cfg.EgressProxy,
		Source:     cfg.Source,
		CACert:     firstNonEmpty(cfg.VaultCACert, os.Getenv("VAULT_CACERT")),
		ClientCert: firstNonEmpty(cfg.VaultCert, os.Getenv("VAULT_CLIENT_CERT")),
		ClientKey:  firstNonEmpty(cfg.VaultKey, os.Getenv("VAULT_CLIENT_KEY")),
	}.HTTPClient(30 * time.Second)
	if err != nil {
		// Fall back to plain client; Call will still set source headers when possible.
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Service{
		cfg:      cfg,
		registry: registry,
		http:     httpClient,
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Call resolves an attached tool, loads Vault creds if needed, guards content,
// and invokes the upstream via the configured transport.
func (s *Service) Call(ctx context.Context, req CallRequest) (*CallResponse, *ErrorBody) {
	toolName := strings.TrimSpace(req.Tool)
	if toolName == "" {
		return nil, &ErrorBody{
			Code:      CodeToolNotFound,
			Message:   "tool name is required",
			NeedHuman: true,
		}
	}

	server, tool, ok := s.registry.LookupTool(toolName)
	if !ok {
		return nil, &ErrorBody{
			Code:      CodeToolNotFound,
			Message:   fmt.Sprintf("MCP tool %q is not attached; attach it or choose another tool", toolName),
			Tool:      toolName,
			NeedHuman: true,
		}
	}

	if !sourceAllowed(server, req.Source) {
		return nil, &ErrorBody{
			Code:      CodeToolNotAllowed,
			Message:   fmt.Sprintf("source %q may not call tools on server %q", req.Source, server.ID),
			Tool:      toolName,
			NeedHuman: true,
		}
	}

	argsText := string(rawOrEmptyObject(req.Arguments))
	guardName := tool.Name
	if tool.Egress {
		guardName = "send-" + tool.Name
	}
	if s.cfg.ContentGuards {
		pre, err := hooks.ProtectToolCall(guardName, argsText)
		if err != nil || pre.Blocked {
			msg := "tool arguments blocked by content guard"
			if pre.Reason != "" {
				msg = pre.Reason
			}
			return nil, &ErrorBody{
				Code:      CodeContentBlocked,
				Message:   msg,
				Tool:      toolName,
				NeedHuman: true,
			}
		}
		argsText = pre.Text
	}

	token, err := s.credential(ctx, server)
	if err != nil {
		return nil, &ErrorBody{
			Code:      CodeVault,
			Message:   err.Error(),
			Tool:      toolName,
			NeedHuman: true,
		}
	}

	result, err := s.transportFor(server).Call(ctx, server, tool, json.RawMessage(argsText), token)
	if err != nil {
		return nil, &ErrorBody{
			Code:      CodeUpstream,
			Message:   err.Error(),
			Tool:      toolName,
			NeedHuman: false,
		}
	}

	if s.cfg.ContentGuards {
		post, err := hooks.ProtectPostLLM(result)
		if err != nil || post.Blocked {
			msg := "tool result blocked by content guard"
			if post.Reason != "" {
				msg = post.Reason
			}
			return nil, &ErrorBody{
				Code:      CodeContentBlocked,
				Message:   msg,
				Tool:      toolName,
				NeedHuman: true,
			}
		}
		result = post.Text
	}

	return &CallResponse{
		Tool:   tool.Name,
		Server: server.ID,
		Result: result,
	}, nil
}

// Registry exposes the attach catalog (tests / admin).
func (s *Service) Registry() *Registry { return s.registry }

func sourceAllowed(server ServerSpec, source string) bool {
	if len(server.AllowedSources) == 0 {
		return true
	}
	source = strings.TrimSpace(source)
	for _, a := range server.AllowedSources {
		if a == source {
			return true
		}
	}
	return false
}

// AdminAuthorized checks MCP attach/detach admin token.
func AdminAuthorized(r *http.Request) bool {
	want := os.Getenv("SYMAGIOS_MCP_ADMIN_TOKEN")
	if want == "" {
		// Dev default: allow attach only from loopback when unset.
		host := r.RemoteAddr
		return strings.HasPrefix(host, "127.") || strings.HasPrefix(host, "[::1]") || strings.HasPrefix(host, "localhost")
	}
	got := r.Header.Get("X-Symagios-Admin-Token")
	return got != "" && got == want
}
