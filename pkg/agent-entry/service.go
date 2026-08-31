package agententry

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"symagios/encrypt"
	applog "symagios/log"
	"symagios/meshauth"
	"symagios/storage/sqlite"
)

// Config for the agent-entry (TUS) HTTP server.
type Config struct {
	Listen string // e.g. ":8443" (TLS)

	// TLS enables east-west mTLS (Envoy → agent-entry). CA requires client certs.
	TLSCertFile string
	TLSKeyFile  string
	TLSCAFile   string
}

// ConfigFromEnv loads listen + TLS paths from the environment.
func ConfigFromEnv() Config {
	return Config{
		Listen:      envOr("SYMAGIOS_AGENT_ENTRY_LISTEN", ":8443"),
		TLSCertFile: os.Getenv("SYMAGIOS_AGENT_ENTRY_TLS_CERT"),
		TLSKeyFile:  os.Getenv("SYMAGIOS_AGENT_ENTRY_TLS_KEY"),
		TLSCAFile:   envOr("SYMAGIOS_AGENT_ENTRY_TLS_CA", os.Getenv("VAULT_CACERT")),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func (c Config) tlsEnabled() bool {
	return c.TLSCertFile != "" && c.TLSKeyFile != ""
}

// Server accepts TUS agent uploads and creates action_run / workflow_manifest rows.
// This is not A2A — agent-gateway owns A2A.
type Server struct {
	db         *sqlite.Client
	encryptKey *encrypt.EncryptKey
	server     *http.Server
	log        *applog.Logger

	uploadsMu sync.Mutex
	uploads   map[string]*pendingUpload // key: session\0agent — in-memory until finalize
}

// New creates an agent-entry server.
func New(db *sqlite.Client, encryptKey *encrypt.EncryptKey) *Server {
	if encryptKey == nil {
		applog.Default().Error("encrypt key missing; agent data will not be encrypted")
	}
	return &Server{
		db:         db,
		encryptKey: encryptKey,
		log:        applog.Default().With("component", "agent-entry"),
		uploads:    make(map[string]*pendingUpload),
	}
}

// Start serves TUS /agents routes.
func (s *Server) Start(ctx context.Context, cfg Config) error {
	listen := cfg.Listen
	if listen == "" {
		listen = ":8443"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("OPTIONS /agents", s.handleAgentsOptions)
	mux.HandleFunc("POST /agents", s.handleAgentsCreate)
	mux.HandleFunc("HEAD /agents/{session_id}/{agent_name}", s.handleAgentUploadHead)
	mux.HandleFunc("PATCH /agents/{session_id}/{agent_name}", s.handleAgentUploadPatch)
	mux.HandleFunc("DELETE /agents/{session_id}/{agent_name}", s.handleAgentUploadDelete)

	s.server = &http.Server{
		Addr:    listen,
		Handler: mux,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}
	if cfg.tlsEnabled() {
		tlsCfg, err := loadServerTLS(cfg.TLSCertFile, cfg.TLSKeyFile, cfg.TLSCAFile)
		if err != nil {
			return fmt.Errorf("agent-entry tls: %w", err)
		}
		s.server.TLSConfig = tlsCfg
	}

	go func() {
		var err error
		if cfg.tlsEnabled() {
			err = s.server.ListenAndServeTLS("", "")
		} else {
			err = s.server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			s.log.Error("http server stopped", "error", err)
		}
	}()

	s.log.Info("agent-entry started", "addr", listen, "tls", cfg.tlsEnabled())
	return nil
}

// Stop shuts down the HTTP server.
func (s *Server) Stop() {
	if s.server == nil {
		return
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.server.Shutdown(shutdownCtx); err != nil {
		s.log.Error("http server shutdown failed", "error", err)
		return
	}
	s.log.Info("agent-entry stopped")
}

func (s *Server) authorize(r *http.Request) bool {
	return meshauth.Authorize(r)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
