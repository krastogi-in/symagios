package log

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Config controls logger output. Storage destination can be wired in later.
type Config struct {
	Level  string
	Format string
	Output io.Writer
}

// Logger wraps slog for structured logging across Symagios.
type Logger struct {
	*slog.Logger
}

var (
	mu            sync.RWMutex
	defaultLogger = newLogger(Config{})
)

func newLogger(cfg Config) *Logger {
	level := parseLevel(cfg.Level)
	writer := cfg.Output
	if writer == nil {
		writer = os.Stdout
	}

	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: level}
	switch strings.ToLower(cfg.Format) {
	case "text":
		handler = slog.NewTextHandler(writer, opts)
	default:
		handler = slog.NewJSONHandler(writer, opts)
	}

	return &Logger{Logger: slog.New(handler)}
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Init replaces the process-wide logger. Call once at startup.
func Init(cfg Config) *Logger {
	mu.Lock()
	defer mu.Unlock()
	defaultLogger = newLogger(cfg)
	return defaultLogger
}

// Default returns the process-wide logger.
func Default() *Logger {
	mu.RLock()
	defer mu.RUnlock()
	return defaultLogger
}

// With returns a child logger with structured fields.
func (l *Logger) With(args ...any) *Logger {
	if l == nil || l.Logger == nil {
		return Default()
	}
	return &Logger{Logger: l.Logger.With(args...)}
}

func (l *Logger) Debug(msg string, args ...any) {
	l.log(slog.LevelDebug, msg, args...)
}

func (l *Logger) Info(msg string, args ...any) {
	l.log(slog.LevelInfo, msg, args...)
}

func (l *Logger) Warn(msg string, args ...any) {
	l.log(slog.LevelWarn, msg, args...)
}

func (l *Logger) Error(msg string, args ...any) {
	l.log(slog.LevelError, msg, args...)
}

func (l *Logger) log(level slog.Level, msg string, args ...any) {
	if l == nil || l.Logger == nil {
		Default().log(level, msg, args...)
		return
	}
	l.Logger.Log(nil, level, msg, args...)
}

// ActionFields returns common action-run log attributes.
func ActionFields(sessionID, actionName string, actionType any) []any {
	return []any{
		"session_id", sessionID,
		"action_name", actionName,
		"action_type", actionType,
	}
}
