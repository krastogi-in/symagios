package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"symagios/utils/types"

	_ "modernc.org/sqlite"
)

const defaultDriverName = "sqlite"

// Config holds SQLite connection settings. Fill in fields as needed.
type Config struct {
	// Path is the SQLite database file path, or ":memory:" for an in-memory DB.
	Path string

	// BusyTimeout controls how long SQLite waits on locked pages.
	BusyTimeout time.Duration
}

// Client implements storage.Store using SQLite.
type Client struct {
	db     *sql.DB
	config Config
}


// New opens (or creates) a SQLite database and applies schema migrations.
func New(cfg Config) (*Client, error) {
	if cfg.Path == "" {
		cfg.Path = "symagios.db"
	}

	if cfg.BusyTimeout <= 0 {
		cfg.BusyTimeout = 5 * time.Second
	}

	if cfg.Path != ":memory:" {
		f, err := os.OpenFile(cfg.Path, os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create database file: %w", err)
		}
		_ = f.Close()
		if err := os.Chmod(cfg.Path, 0o600); err != nil {
			return nil, fmt.Errorf("change database file permissions: %w", err)
		}
	}

	dsn := fmt.Sprintf("file:%s?"+
		"_pragma=journal_mode(WAL)&"+
		"_pragma=busy_timeout(%d)&"+
		"_pragma=foreign_keys(ON)&"+
		"_pragma=trusted_schema(OFF)",
		 cfg.Path, cfg.BusyTimeout.Milliseconds())

	db, err := sql.Open(defaultDriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)


	client := &Client{db: db, config: cfg}
	if err := client.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}

	return client, nil
}

func (c *Client) migrate(ctx context.Context) error {
	if _, err := c.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("apply sqlite schema: %w", err)
	}
	return nil
}

// Close closes the underlying database handle.
func (c *Client) Close() error {
	if c.db == nil {
		return nil
	}
	return c.db.Close()
}

func isTerminalStatus(status types.ActionRunStatus) bool {
	switch status {
	case types.Completed, types.Failed:
		return true
	default:
		return false
	}
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
