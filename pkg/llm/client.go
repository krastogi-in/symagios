package llm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"symagios/config"
	"symagios/egress"
)

// Provider selects which backend to use.
type Provider string

const (
	ProviderClaude       Provider = "claude"
	ProviderClaudeVertex Provider = "claude-vertex"
	ProviderOpenCodex    Provider = "opencodex"
)

// Message is one turn in a chat.
type Message struct {
	Role    string `json:"role"` // system | user | assistant
	Content string `json:"content"`
}

// Request is a single completion call.
// When GovernedSearch is set, the provider receives only Symagios's custom
// web_search tool. Anthropic/OpenAI server-side search tools are never sent,
// so a live search can only happen through GovernedSearch (Tavily).
type Request struct {
	System    string
	Messages  []Message
	Model     string
	MaxTokens int

	GovernedSearch  func(ctx context.Context, query string) (string, error)
	MaxSearchRounds int
}

// Response is the model completion (after content guards should be applied by caller).
type Response struct {
	Text  string
	Model string
	Raw   string // provider payload for debugging (never log secrets)
}

// Client talks to an LLM provider.
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
	Provider() Provider
}

// Config matches the llm: block in deploy/llm/config.yaml.
// Missing fields get defaults via applyDefaults. API keys come from env / Vault.
type Config struct {
	Provider   Provider `yaml:"provider"`
	APIKey     string   `yaml:"api_key"`
	BaseURL    string   `yaml:"base_url"`
	Model      string   `yaml:"model"`
	MaxTokens  int      `yaml:"max_tokens"`
	VaultPath  string   `yaml:"vault_path"`
	VaultField string   `yaml:"vault_field"`

	// Vertex (provider: claude-vertex): GCP project + location for :rawPredict.
	Project  string `yaml:"project"`
	Location string `yaml:"location"`

	// EgressProxy, when set, routes provider HTTP through Envoy egress.
	// Empty = direct dial (unit tests). Compose sets SYMAGIOS_EGRESS_PROXY.
	EgressProxy string `yaml:"egress_proxy"`
	Source      string `yaml:"source"` // X-Symagios-Source (default symagios-engine)
}

// ConfigFromFile loads deploy/llm/config.yaml (or path) and fills missing defaults.
func ConfigFromFile(path string) (Config, error) {
	if path == "" {
		path = config.DefaultLLMConfigPath()
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var root struct {
		LLM Config `yaml:"llm"`
	}
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return Config{}, fmt.Errorf("parse llm config %s: %w", path, err)
	}
	cfg := root.LLM
	cfg.applyDefaults()
	return cfg, nil
}

// applyDefaults fills empty fields; YAML values always win when set.
func (c *Config) applyDefaults() {
	c.Provider = Provider(strings.ToLower(strings.TrimSpace(string(c.Provider))))
	if c.Provider == "" {
		c.Provider = ProviderClaude
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = 4096
	}
	if c.VaultPath == "" {
		c.VaultPath = "symagios/llm"
	}
	if c.VaultField == "" {
		if c.Provider == ProviderClaudeVertex {
			c.VaultField = "access_token"
		} else {
			c.VaultField = "api_key"
		}
	}
	if c.Source == "" {
		c.Source = firstNonEmpty(os.Getenv("SYMAGIOS_SOURCE"), "symagios-engine")
	}
	if c.EgressProxy == "" {
		c.EgressProxy = os.Getenv("SYMAGIOS_EGRESS_PROXY")
	}
	switch c.Provider {
	case ProviderOpenCodex:
		if c.BaseURL == "" {
			c.BaseURL = "https://api.openai.com/v1"
		}
		if c.Model == "" {
			c.Model = "gpt-5-codex"
		}
	case ProviderClaudeVertex:
		if c.Project == "" {
			c.Project = firstNonEmpty(os.Getenv("VERTEX_PROJECT"), os.Getenv("GOOGLE_CLOUD_PROJECT"), os.Getenv("GCP_PROJECT"))
		}
		if c.Location == "" {
			c.Location = firstNonEmpty(os.Getenv("VERTEX_LOCATION"), os.Getenv("GOOGLE_CLOUD_REGION"), "us-east5")
		}
		if c.Model == "" {
			c.Model = "claude-sonnet-4@20250514"
		}
	case ProviderClaude:
		if c.BaseURL == "" {
			c.BaseURL = "https://api.anthropic.com"
		}
		if c.Model == "" {
			c.Model = "claude-sonnet-4-20250514"
		}
	}
}

// New builds a Client. YAML supplies non-secrets; API key / Vertex token from cfg, else env, else Vault / ADC.
func New(ctx context.Context, cfg Config) (Client, error) {
	cfg.applyDefaults()
	if cfg.APIKey == "" {
		cfg.APIKey = apiKeyFromEnv(cfg.Provider)
	}
	if cfg.APIKey == "" && cfg.Provider != ProviderClaudeVertex {
		key, err := apiKeyFromVault(ctx, cfg)
		if err == nil {
			cfg.APIKey = key
		}
	}
	if cfg.Provider == ProviderClaudeVertex {
		if cfg.Project == "" || cfg.Location == "" {
			return nil, fmt.Errorf("llm: claude-vertex requires project and location (yaml or VERTEX_PROJECT / VERTEX_LOCATION)")
		}
		// Token may come from env, Vault, or ADC at request time.
		if cfg.APIKey == "" {
			if key, err := apiKeyFromVault(ctx, cfg); err == nil {
				cfg.APIKey = key
			}
		}
	} else if cfg.APIKey == "" {
		return nil, fmt.Errorf("llm: no API key (set provider env key or Vault %s/%s)", cfg.VaultPath, cfg.VaultField)
	}
	httpClient, err := egress.Config{
		Proxy:      cfg.EgressProxy,
		Source:     cfg.Source,
		CACert:     os.Getenv("VAULT_CACERT"),
		ClientCert: os.Getenv("VAULT_CLIENT_CERT"),
		ClientKey:  os.Getenv("VAULT_CLIENT_KEY"),
	}.HTTPClient(120 * time.Second)
	if err != nil {
		return nil, fmt.Errorf("llm egress client: %w", err)
	}
	switch cfg.Provider {
	case ProviderOpenCodex:
		return newOpenCodex(cfg, httpClient), nil
	case ProviderClaude, ProviderClaudeVertex:
		return newClaude(cfg, httpClient), nil
	default:
		return nil, fmt.Errorf("llm: unknown provider %q (use claude, claude-vertex, or opencodex)", cfg.Provider)
	}
}

func apiKeyFromEnv(p Provider) string {
	switch p {
	case ProviderClaude:
		return firstNonEmpty(os.Getenv("ANTHROPIC_API_KEY"), os.Getenv("CLAUDE_API_KEY"), os.Getenv("SYMAGIOS_LLM_API_KEY"))
	case ProviderClaudeVertex:
		return firstNonEmpty(os.Getenv("VERTEX_ACCESS_TOKEN"), os.Getenv("GOOGLE_OAUTH_ACCESS_TOKEN"), os.Getenv("SYMAGIOS_LLM_API_KEY"))
	case ProviderOpenCodex:
		return firstNonEmpty(os.Getenv("OPENCODEX_API_KEY"), os.Getenv("OPENAI_API_KEY"), os.Getenv("SYMAGIOS_LLM_API_KEY"))
	default:
		return os.Getenv("SYMAGIOS_LLM_API_KEY")
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
