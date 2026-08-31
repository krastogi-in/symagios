package egress

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultProxy = "https://envoy-egress:10001"

// Config holds egress proxy settings and optional mTLS material for Envoy.
type Config struct {
	Proxy      string
	Source     string
	CACert     string
	ClientCert string
	ClientKey  string
}

// FromEnv loads SYMAGIOS_EGRESS_PROXY / VAULT_* / optional source default.
// When requireProxy is true and the env is unset, Proxy defaults to the compose egress URL.
func FromEnv(defaultSource string, requireProxy bool) Config {
	proxy := os.Getenv("SYMAGIOS_EGRESS_PROXY")
	if proxy == "" && requireProxy {
		proxy = defaultProxy
	}
	source := os.Getenv("SYMAGIOS_SOURCE")
	if source == "" {
		source = defaultSource
	}
	return Config{
		Proxy:      proxy,
		Source:     source,
		CACert:     os.Getenv("VAULT_CACERT"),
		ClientCert: os.Getenv("VAULT_CLIENT_CERT"),
		ClientKey:  os.Getenv("VAULT_CLIENT_KEY"),
	}
}

// Enabled is true when outbound calls should use the egress proxy.
func (c Config) Enabled() bool {
	return strings.TrimSpace(c.Proxy) != ""
}

// HTTPClient returns a client that reaches HTTPS origins via CONNECT through
// the egress proxy (Envoy's connect_matcher route). mTLS to the proxy uses the
// Vault workload cert; origin TLS uses the system trust store (+ Vault CA).
func (c Config) HTTPClient(timeout time.Duration) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}

	if c.ClientCert != "" || c.ClientKey != "" || c.CACert != "" {
		tlsCfg, err := clientTLS(c.ClientCert, c.ClientKey, c.CACert)
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig = tlsCfg
	}

	if proxy := strings.TrimSpace(c.Proxy); proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("egress proxy URL: %w", err)
		}
		// CONNECT for https:// origins — required so Google/Anthropic see TLS.
		transport.Proxy = http.ProxyURL(u)
		// Envoy egress advertises h2; Go's HTTP/2 CONNECT to an HTTPS proxy is
		// flaky (unexpected EOF). Prefer HTTP/1.1 to the proxy; origin TLS is fine.
		transport.ForceAttemptHTTP2 = false
		transport.TLSNextProto = map[string]func(authority string, c *tls.Conn) http.RoundTripper{}
	}

	return &http.Client{Timeout: timeout, Transport: transport}, nil
}

func clientTLS(certFile, keyFile, caFile string) (*tls.Config, error) {
	// System roots for public origins (Vertex, Anthropic, …); Vault CA for egress mTLS.
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("egress CA: %w", err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("egress CA: parse %s", caFile)
		}
	}
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("egress client cert: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

// SetSource stamps X-Symagios-Source for mesh audit. Routing through Envoy
// egress is configured on the http.Client (Transport.Proxy), not here.
func SetSource(req *http.Request, source string) error {
	if source != "" {
		req.Header.Set("X-Symagios-Source", source)
	}
	if req.URL == nil || req.URL.Host == "" {
		return fmt.Errorf("egress: request URL missing host")
	}
	return nil
}
