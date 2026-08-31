package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	mcpgateway "symagios/mcp-gateway"
)

func main() {
	cfg := mcpgateway.ConfigFromEnv()
	regPath := cfg.RegistryPath
	if !filepath.IsAbs(regPath) {
		if _, err := os.Stat(regPath); err != nil {
			// Try repo-root relative when running from pkg/
			alt := filepath.Join("..", regPath)
			if _, err2 := os.Stat(alt); err2 == nil {
				regPath = alt
			}
		}
	}

	reg, err := mcpgateway.LoadRegistry(regPath)
	if err != nil {
		log.Fatalf("mcp registry: %v", err)
	}

	svc := mcpgateway.New(cfg, reg)
	mux := http.NewServeMux()
	mcpgateway.NewHandler(svc).Mount(mux)

	srv := &http.Server{Addr: cfg.Listen, Handler: mux}
	fmt.Printf("secured mcp-gateway listening on %s tls=%v (registry=%s, tools=%d)\n",
		cfg.Listen, cfg.TLSEnabled(), regPath, len(reg.ListTools()))

	if cfg.TLSEnabled() {
		tlsCfg, err := mcpgateway.LoadServerTLS(cfg.TLSCertFile, cfg.TLSKeyFile, cfg.TLSCAFile)
		if err != nil {
			log.Fatalf("mcp-gateway tls: %v", err)
		}
		srv.TLSConfig = tlsCfg
		log.Fatal(srv.ListenAndServeTLS("", ""))
	}
	log.Fatal(srv.ListenAndServe())
}
