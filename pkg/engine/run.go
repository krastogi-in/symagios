package engine

import (
	"context"

	"symagios/encrypt"
	"symagios/engine/hooks"
	"symagios/storage/sqlite"
)

// RunConfig controls engine startup.
type RunConfig struct {
	Workers int
}

// Engine runs the orchestration loop against a SQLite store.
type Engine struct {
	db        *sqlite.Client
	encryptKey *encrypt.EncryptKey
	cancel    context.CancelFunc
}

// New creates an engine bound to the given store client.
func New(db *sqlite.Client, encryptKey *encrypt.EncryptKey) *Engine {
	return &Engine{db: db, encryptKey: encryptKey}
}

// InitStore sets the package-level store (+ encrypt key) for SubmitHumanInput.
// Use this in standalone processes that share the engine SQLite file (e.g. watch).
func InitStore(db *sqlite.Client, encryptKey *encrypt.EncryptKey) {
	store = db
	storeEncrypt = encryptKey
}

// Start initializes workers and the scheduler.
func (e *Engine) Start(ctx context.Context, cfg RunConfig) {
	workers := cfg.Workers
	if workers <= 0 {
		workers = 4
	}

	ctx, cancel := context.WithCancel(ctx)
	e.cancel = cancel
	startControl(ctx, e.db, e.encryptKey, workers)
}

// Stop cancels the engine context and releases eBPF resources.
func (e *Engine) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
	hooks.CloseLSM()
}

// DB returns the underlying store client.
func (e *Engine) DB() *sqlite.Client {
	return e.db
}
