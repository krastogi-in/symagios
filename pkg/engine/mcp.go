package engine

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// MCPCallOutcome is the engine-facing result of asking mcp-gateway to run a tool.
type MCPCallOutcome struct {
	Result    string
	Server    string
	NeedHuman bool
	Code      string
	Message   string
}

var (
	mcpClientOnce sync.Once
	mcpHTTPClient *http.Client
)

func mcpGatewayBase() string {
	base := os.Getenv("SYMAGIOS_MCP_GATEWAY")
	if base != "" {
		return strings.TrimRight(base, "/")
	}
	return "https://mcp-gateway:8443"
}

func mcpClient() *http.Client {
	mcpClientOnce.Do(func() {
		mcpHTTPClient = &http.Client{Timeout: 30 * time.Second}
		cert := envFirst("SYMAGIOS_MCP_CLIENT_CERT", "VAULT_CLIENT_CERT")
		key := envFirst("SYMAGIOS_MCP_CLIENT_KEY", "VAULT_CLIENT_KEY")
		ca := envFirst("SYMAGIOS_MCP_CA", "VAULT_CACERT")
		if cert == "" || key == "" {
			return
		}
		tlsCfg, err := loadMTLSClient(cert, key, ca)
		if err != nil {
			executeLogger.Error("mcp client tls config failed; using plain HTTP client", "error", err)
			return
		}
		mcpHTTPClient.Transport = &http.Transport{TLSClientConfig: tlsCfg}
	})
	return mcpHTTPClient
}

func envFirst(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func loadMTLSClient(certFile, keyFile, caFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse CA pem: %s", caFile)
		}
		cfg.RootCAs = pool
	}
	return cfg, nil
}

// CallMCPTool POSTs to the secured mcp-gateway. If the tool is not attached
// (or content/Vault blocks), NeedHuman is true and the caller should set the
// action run to WaitingForHumanInput.
func CallMCPTool(ctx context.Context, tool string, arguments json.RawMessage) (MCPCallOutcome, error) {
	base := mcpGatewayBase()
	payload, _ := json.Marshal(map[string]any{
		"tool":      tool,
		"arguments": arguments,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/mcp/tools/call", bytes.NewReader(payload))
	if err != nil {
		return MCPCallOutcome{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := mcpClient().Do(req)
	if err != nil {
		return MCPCallOutcome{}, fmt.Errorf("mcp-gateway: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return MCPCallOutcome{}, err
	}

	if resp.StatusCode == http.StatusOK {
		var ok struct {
			Server string `json:"server"`
			Result string `json:"result"`
		}
		if err := json.Unmarshal(raw, &ok); err != nil {
			return MCPCallOutcome{}, err
		}
		return MCPCallOutcome{Result: ok.Result, Server: ok.Server}, nil
	}

	var eb struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		NeedHuman bool   `json:"need_human"`
	}
	_ = json.Unmarshal(raw, &eb)
	msg := eb.Message
	if msg == "" {
		msg = string(raw)
	}
	return MCPCallOutcome{
		NeedHuman: eb.NeedHuman || resp.StatusCode == http.StatusConflict,
		Code:      eb.Code,
		Message:   msg,
	}, nil
}
