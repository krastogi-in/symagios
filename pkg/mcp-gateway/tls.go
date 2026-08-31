package mcpgateway

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"symagios/meshauth"
)

// LoadServerTLS builds a tls.Config for east-west mTLS (engine → mcp-gateway).
// Only the engine workload client cert is accepted.
func LoadServerTLS(certFile, keyFile, caFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load server keypair: %w", err)
	}
	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	if caFile == "" {
		return cfg, nil
	}
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("parse CA pem: %s", caFile)
	}
	cfg.ClientCAs = pool
	cfg.ClientAuth = tls.RequireAndVerifyClientCert
	meshauth.RequireEngineClient(cfg)
	return cfg, nil
}
