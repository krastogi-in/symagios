package searchgateway

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"

	"symagios/egress"
	"symagios/meshauth"
)

// LoadServerTLS builds tls.Config for east-west mTLS (engine → search-broker).
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

func newHTTPClient(cfg Config) (*http.Client, error) {
	return egress.Config{
		Proxy:      cfg.EgressProxy,
		Source:     cfg.SourceService,
		CACert:     cfg.ClientCAFile,
		ClientCert: cfg.ClientCertFile,
		ClientKey:  cfg.ClientKeyFile,
	}.HTTPClient(45 * time.Second)
}
