package agentgateway

import (
	"os"
	"strings"
)

// GatewayConfig describes this gateway instance for on-server A2A tasks.
type GatewayConfig struct {
	AgentName   string
	Version     string
	URL         string
	Host        string
	EgressProxy string // e.g. https://envoy-egress:10001

	// ExternalHosts are peer A2A bases seeded at start
	// (from SYMAGIOS_AGENT_GATEWAY_EXTERNAL_HOSTS or --external-hosts).
	ExternalHosts []string

	// CA enables RequireAndVerifyClientCert; A2A allows envoy-ingress + peer gateways.
	TLSCertFile string
	TLSKeyFile  string
	TLSCAFile   string
}

// ConfigFromEnv loads gateway settings from the environment.
func ConfigFromEnv() GatewayConfig {
	return GatewayConfig{
		AgentName:     envOr("SYMAGIOS_AGENT_GATEWAY_NAME", "symagios"),
		Version:       envOr("SYMAGIOS_AGENT_GATEWAY_VERSION", "0.1.0"),
		URL:           os.Getenv("SYMAGIOS_AGENT_GATEWAY_URL"),
		EgressProxy:   envOr("SYMAGIOS_EGRESS_PROXY", "https://envoy-egress:10001"),
		ExternalHosts: splitCSV(os.Getenv("SYMAGIOS_AGENT_GATEWAY_EXTERNAL_HOSTS")),
		TLSCertFile:   os.Getenv("SYMAGIOS_AGENT_GATEWAY_TLS_CERT"),
		TLSKeyFile:    os.Getenv("SYMAGIOS_AGENT_GATEWAY_TLS_KEY"),
		TLSCAFile:     envOr("SYMAGIOS_AGENT_GATEWAY_TLS_CA", os.Getenv("VAULT_CACERT")),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitCSV(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (c GatewayConfig) tlsEnabled() bool {
	return c.TLSCertFile != "" && c.TLSKeyFile != ""
}
