package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	searchgateway "symagios/search-gateway"
)

func main() {
	cfg := searchgateway.ConfigFromEnv()
	client, err := searchgateway.NewClient(context.Background(), cfg)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/v1/search", searchgateway.NewHandler(client))

	srv := &http.Server{Addr: cfg.Listen, Handler: mux}
	fmt.Printf("governed search broker listening on %s tls=%v\n", cfg.Listen, cfg.TLSEnabled())

	if cfg.TLSEnabled() {
		tlsCfg, err := searchgateway.LoadServerTLS(cfg.TLSCertFile, cfg.TLSKeyFile, cfg.TLSCAFile)
		if err != nil {
			log.Fatalf("search-broker tls: %v", err)
		}
		srv.TLSConfig = tlsCfg
		log.Fatal(srv.ListenAndServeTLS("", ""))
	}
	log.Fatal(srv.ListenAndServe())
}
