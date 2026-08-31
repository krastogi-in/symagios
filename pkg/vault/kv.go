package vault

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Config for Vault KV v2 reads. When ClientCert/Key are set, mTLS is used
// (e.g. https://vault.symagios.local:8202).
type Config struct {
	Addr       string
	Token      string
	CACert     string
	ClientCert string
	ClientKey  string
	// Path without secret/ prefix, e.g. symagios/engine
	Path string
}

type kvResponse struct {
	Data struct {
		Data map[string]string `json:"data"`
	} `json:"data"`
}

// GetField reads one field from a KV v2 secret.
func GetField(ctx context.Context, cfg Config, field string) (string, error) {
	if cfg.Addr == "" || cfg.Token == "" {
		return "", fmt.Errorf("vault addr and token are required")
	}
	if cfg.Path == "" {
		return "", fmt.Errorf("vault path is required")
	}
	if field == "" {
		return "", fmt.Errorf("field is required")
	}

	url := fmt.Sprintf("%s/v1/secret/data/%s", strings.TrimRight(cfg.Addr, "/"), strings.TrimPrefix(cfg.Path, "secret/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Vault-Token", cfg.Token)

	client, err := httpClient(cfg)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vault read %s: %s", cfg.Path, resp.Status)
	}

	var parsed kvResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	value := parsed.Data.Data[field]
	if value == "" {
		return "", fmt.Errorf("field %q missing at secret/%s", field, cfg.Path)
	}
	return value, nil
}

// ConfigFromEnv builds Config from standard Vault / Symagios env vars.
func ConfigFromEnv() Config {
	return Config{
		Addr:       firstNonEmpty(os.Getenv("VAULT_ADDR"), "https://vault.symagios.local:8202"),
		Token:      os.Getenv("VAULT_TOKEN"),
		CACert:     os.Getenv("VAULT_CACERT"),
		ClientCert: os.Getenv("VAULT_CLIENT_CERT"),
		ClientKey:  os.Getenv("VAULT_CLIENT_KEY"),
		Path:       firstNonEmpty(os.Getenv("SYMAGIOS_VAULT_ENGINE_PATH"), "symagios/engine"),
	}
}

func httpClient(cfg Config) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}

	if cfg.CACert != "" {
		pem, err := os.ReadFile(cfg.CACert)
		if err != nil {
			return nil, fmt.Errorf("read VAULT_CACERT: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse VAULT_CACERT")
		}
		tlsCfg.RootCAs = pool
	}

	if cfg.ClientCert != "" && cfg.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCert, cfg.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("load client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	transport.TLSClientConfig = tlsCfg
	return &http.Client{Timeout: 10 * time.Second, Transport: transport}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
