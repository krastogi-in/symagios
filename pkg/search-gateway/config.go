package searchgateway

import "os"

const (
	defaultListen       = ":8443"
	defaultTavilyHost   = "api.tavily.com"
	defaultEgressProxy  = "https://envoy-egress:10001"
	defaultVaultAddr    = "https://vault.symagios.local:8202"
	defaultVaultKeyPath = "symagios/tavily"
	defaultSource       = "symagios-engine"
)

// Config controls governed web search through the egress gateway.
type Config struct {
	Listen        string
	EgressProxy   string
	TavilyHost    string
	SourceService string
	VaultAddr     string
	VaultToken    string
	VaultKeyPath  string
	APIKey        string // dev fallback when Vault is unavailable

	// TLS (east-west mTLS). When Cert+Key are set, ListenAndServeTLS is used.
	// CA enables RequireAndVerifyClientCert (engine presents workload cert).
	TLSCertFile string
	TLSKeyFile  string
	TLSCAFile   string

	// Outbound mTLS to Vault and envoy-egress (reuse workload cert / VAULT_*).
	ClientCertFile string
	ClientKeyFile  string
	ClientCAFile   string
}

func ConfigFromEnv() Config {
	return Config{
		Listen:         env("SYMAGIOS_SEARCH_LISTEN", defaultListen),
		EgressProxy:    env("SYMAGIOS_EGRESS_PROXY", defaultEgressProxy),
		TavilyHost:     env("TAVILY_HOST", defaultTavilyHost),
		SourceService:  env("SYMAGIOS_SOURCE", defaultSource),
		VaultAddr:      env("VAULT_ADDR", defaultVaultAddr),
		VaultToken:     os.Getenv("VAULT_TOKEN"),
		VaultKeyPath:   env("VAULT_TAVILY_PATH", defaultVaultKeyPath),
		APIKey:         env("TAVILY_API_KEY", ""),
		TLSCertFile:    os.Getenv("SYMAGIOS_SEARCH_TLS_CERT"),
		TLSKeyFile:     os.Getenv("SYMAGIOS_SEARCH_TLS_KEY"),
		TLSCAFile:      env("SYMAGIOS_SEARCH_TLS_CA", os.Getenv("VAULT_CACERT")),
		ClientCertFile: os.Getenv("VAULT_CLIENT_CERT"),
		ClientKeyFile:  os.Getenv("VAULT_CLIENT_KEY"),
		ClientCAFile:   os.Getenv("VAULT_CACERT"),
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
