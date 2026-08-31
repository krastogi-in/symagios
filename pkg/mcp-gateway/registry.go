package mcpgateway

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// RegistryFile is the on-disk attach catalog. New MCPs become usable only after
// they appear here (or via Attach at runtime).
type RegistryFile struct {
	Servers []ServerSpec `json:"servers"`
}

// ServerSpec describes one attached MCP upstream.
type ServerSpec struct {
	ID          string     `json:"id"`
	Enabled     bool       `json:"enabled"`
	Transport   string     `json:"transport,omitempty"` // stub | http | sse | stdio
	BaseURL     string     `json:"base_url"`             // http(s)://…, stub://…, or stdio://binary
	MessagesURL string     `json:"messages_url,omitempty"`
	Command     string     `json:"command,omitempty"` // stdio binary
	Args        []string   `json:"args,omitempty"`
	Env         []string   `json:"env,omitempty"` // KEY=VAL
	VaultPath   string     `json:"vault_path,omitempty"`
	VaultField  string     `json:"vault_field,omitempty"` // default "token"
	AuthHeader  string     `json:"auth_header,omitempty"` // default "Authorization"
	AuthPrefix  string     `json:"auth_prefix,omitempty"` // default "Bearer "
	Tools       []ToolSpec `json:"tools"`
	Description string     `json:"description,omitempty"`
	// AllowedSources restricts which x-symagios-source values may call tools on this server.
	AllowedSources []string `json:"allowed_sources,omitempty"`
}

// ToolSpec is an allowlisted tool on an attached server.
type ToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Egress marks tools that can leave the trust boundary (email, http, …).
	Egress bool `json:"egress,omitempty"`
}

// Registry is the thread-safe in-memory attach catalog.
type Registry struct {
	mu      sync.RWMutex
	servers map[string]ServerSpec // id -> server
	tools   map[string]toolRef    // tool name -> server+tool
}

type toolRef struct {
	Server ServerSpec
	Tool   ToolSpec
}

// LoadRegistry reads a JSON registry file.
func LoadRegistry(path string) (*Registry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mcp registry: %w", err)
	}
	var file RegistryFile
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, fmt.Errorf("parse mcp registry: %w", err)
	}
	r := NewRegistry()
	for _, s := range file.Servers {
		if err := r.Attach(s); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func NewRegistry() *Registry {
	return &Registry{
		servers: map[string]ServerSpec{},
		tools:   map[string]toolRef{},
	}
}

// Attach registers or replaces a server. Tools with duplicate names across
// servers are rejected so agents never get an ambiguous catalog.
func (r *Registry) Attach(s ServerSpec) error {
	s.ID = strings.TrimSpace(s.ID)
	if s.ID == "" {
		return fmt.Errorf("server id is required")
	}
	if s.VaultField == "" {
		s.VaultField = "token"
	}
	if s.AuthHeader == "" {
		s.AuthHeader = "Authorization"
	}
	if s.AuthPrefix == "" {
		s.AuthPrefix = "Bearer "
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Drop previous tools for this server id on replace.
	if old, ok := r.servers[s.ID]; ok {
		for _, t := range old.Tools {
			delete(r.tools, t.Name)
		}
	}

	for _, t := range s.Tools {
		t.Name = strings.TrimSpace(t.Name)
		if t.Name == "" {
			return fmt.Errorf("server %q: empty tool name", s.ID)
		}
		if other, ok := r.tools[t.Name]; ok && other.Server.ID != s.ID {
			return fmt.Errorf("tool %q already attached to server %q", t.Name, other.Server.ID)
		}
		r.tools[t.Name] = toolRef{Server: s, Tool: t}
	}
	r.servers[s.ID] = s
	return nil
}

// Detach removes a server and its tools.
func (r *Registry) Detach(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.servers[id]
	if !ok {
		return false
	}
	for _, t := range old.Tools {
		delete(r.tools, t.Name)
	}
	delete(r.servers, id)
	return true
}

// LookupTool returns the attached server/tool or false if missing/disabled.
func (r *Registry) LookupTool(name string) (ServerSpec, ToolSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ref, ok := r.tools[name]
	if !ok || !ref.Server.Enabled {
		return ServerSpec{}, ToolSpec{}, false
	}
	return ref.Server, ref.Tool, true
}

// ListTools returns the agent-visible catalog (enabled servers only).
func (r *Registry) ListTools() []ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ToolSpec, 0, len(r.tools))
	for _, ref := range r.tools {
		if !ref.Server.Enabled {
			continue
		}
		t := ref.Tool
		if t.Description == "" {
			t.Description = fmt.Sprintf("%s/%s", ref.Server.ID, t.Name)
		}
		out = append(out, t)
	}
	return out
}

// ListServers returns attached servers (for admin/debug).
func (r *Registry) ListServers() []ServerSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ServerSpec, 0, len(r.servers))
	for _, s := range r.servers {
		out = append(out, s)
	}
	return out
}
