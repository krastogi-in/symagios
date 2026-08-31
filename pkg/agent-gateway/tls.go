package agentgateway

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"symagios/meshauth"
)

// loadServerTLS builds a tls.Config for east-west mTLS (Envoy → agent-gateway).
// Server presents TLSCertFile/TLSKeyFile; when TLSCAFile is set, client certs are required.
func loadServerTLS(certFile, keyFile, caFile string) (*tls.Config, error) {
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
	// Humans via envoy-ingress (JWT); mesh peers via agent-gateway / envoy-egress.
	meshauth.RequireA2AClients(cfg)
	return cfg, nil
}
