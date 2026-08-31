package mcpgateway

import (
	"os"
)

const (
	defaultListen       = ":8443"
	defaultRegistryPath = "deploy/mcp/registry.json"
	defaultVaultAddr    = "https://vault.symagios.local:8202"
	defaultEgressProxy  = "https://envoy-egress:10001"
	defaultSource       = "mcp-gateway"
)

// Config for the secured MCP gateway.
type Config struct {
	Listen       string
	RegistryPath string
	VaultAddr    string
	VaultToken   string
	VaultCACert  string
	VaultCert    string
	VaultKey     string
	// ContentGuards enables ProtectToolCall / ProtectPostLLM around tool I/O.
	ContentGuards bool

	// TLS (east-west mTLS). When Cert+Key are set, ListenAndServeTLS is used.
	// CA enables RequireAndVerifyClientCert (engine presents workload cert).
	TLSCertFile string
	TLSKeyFile  string
	TLSCAFile   string

	// EgressProxy routes upstream HTTP/SSE MCP calls through Envoy egress.
	// Empty disables rewrite (unit tests). Compose sets SYMAGIOS_EGRESS_PROXY.
	EgressProxy string
	Source      string // X-Symagios-Source for egress OPA
}

func ConfigFromEnv() Config {
	return Config{
		Listen:        env("SYMAGIOS_MCP_LISTEN", defaultListen),
		RegistryPath:  env("SYMAGIOS_MCP_REGISTRY", defaultRegistryPath),
		VaultAddr:     env("VAULT_ADDR", defaultVaultAddr),
		VaultToken:    os.Getenv("VAULT_TOKEN"),
		VaultCACert:   os.Getenv("VAULT_CACERT"),
		VaultCert:     os.Getenv("VAULT_CLIENT_CERT"),
		VaultKey:      os.Getenv("VAULT_CLIENT_KEY"),
		ContentGuards: env("SYMAGIOS_CONTENT_GUARD", "true") != "false",
		TLSCertFile:   os.Getenv("SYMAGIOS_MCP_TLS_CERT"),
		TLSKeyFile:    os.Getenv("SYMAGIOS_MCP_TLS_KEY"),
		TLSCAFile:     env("SYMAGIOS_MCP_TLS_CA", os.Getenv("VAULT_CACERT")),
		EgressProxy:   env("SYMAGIOS_EGRESS_PROXY", defaultEgressProxy),
		Source:        env("SYMAGIOS_SOURCE", defaultSource),
	}
}

func (c Config) TLSEnabled() bool {
	return c.TLSCertFile != "" && c.TLSKeyFile != ""
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
