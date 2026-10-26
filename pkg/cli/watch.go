package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"symagios/config"
	"symagios/encrypt"
	"symagios/engine"
	applog "symagios/log"
	"symagios/storage"
	"symagios/storage/sqlite"
	"symagios/utils/types"
)

type actionSnap struct {
	status    types.ActionRunStatus
	updatedAt time.Time
	resultKey string
}

// Watch polls SQLite for action_runs changes (observability) and prompts for
// WAITING_FOR_HUMAN_INPUT when stdin is a TTY (compose attach / run -it).
// Detached `up -d` has no stdin — prompts are printed once and polling continues
// so HITL never freezes the watcher.
func Watch(ctx context.Context, db *sqlite.Client, encryptKey *encrypt.EncryptKey, cfg config.Watch) error {
	logger := applog.Default().With("component", "cli_watch")
	interval := cfg.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	preview := cfg.MaxPreview
	if preview <= 0 {
		preview = 400
	}

	// Detached compose (up -d) often still has a PTY (tty: true), which makes
	// stdin look interactive while nobody is attached — ReadString then hangs.
	// Default: observe only. Opt in with SYMAGIOS_WATCH_HITL=1 (attach / run -it).
	interactive := false
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SYMAGIOS_WATCH_HITL"))) {
	case "1", "true", "yes", "on":
		interactive = stdinIsInteractive()
		if !interactive {
			fmt.Fprintln(os.Stdout, "SYMAGIOS_WATCH_HITL=1 but stdin is not a TTY — falling back to observe-only")
		}
	default:
		interactive = false
	}
	reader := bufio.NewReader(os.Stdin)
	hitlSeen := make(map[string]struct{})
	hitlNotified := make(map[string]struct{})
	snaps := make(map[string]actionSnap)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info("watch started", "db", cfg.DBPath, "poll", interval.String(), "session", cfg.SessionID, "max_preview", preview, "interactive", interactive)
	fmt.Fprintf(os.Stdout, "watching action_runs (poll=%s interactive=%v)\n", interval, interactive)
	if !interactive {
		fmt.Fprintln(os.Stdout, "HITL observe-only (SYMAGIOS_WATCH_HITL!=1). To answer: SYMAGIOS_WATCH_HITL=1 ./scripts/compose.sh --profile auth --profile watch run --rm --no-deps watch")
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := emitActionChanges(ctx, db, encryptKey, cfg.SessionID, preview, snaps); err != nil {
				return err
			}
			if err := promptHumanInput(ctx, db, encryptKey, reader, hitlSeen, hitlNotified, cfg.SessionID, interactive, logger); err != nil {
				return err
			}
		}
	}
}

func stdinIsInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func emitActionChanges(
	ctx context.Context,
	db *sqlite.Client,
	encryptKey *encrypt.EncryptKey,
	sessionFilter string,
	preview int,
	snaps map[string]actionSnap,
) error {
	runs, err := db.ListAllActionRuns(ctx)
	if err != nil {
		return fmt.Errorf("list action runs: %w", err)
	}
	for _, run := range runs {
		if sessionFilter != "" && run.SessionID != sessionFilter {
			continue
		}
		key := actionKey(run)
		resultPlain := decryptField(encryptKey, run.Result)
		next := actionSnap{
			status:    run.Status,
			updatedAt: run.UpdatedAt,
			resultKey: resultPlain,
		}
		prev, ok := snaps[key]
		if ok && prev.status == next.status && prev.updatedAt.Equal(next.updatedAt) && prev.resultKey == next.resultKey {
			continue
		}

		ts := run.UpdatedAt.UTC().Format("15:04:05")
		if !ok {
			fmt.Fprintf(os.Stdout, "[action %s] NEW  session=%s name=%s type=%s status=%s\n",
				ts, run.SessionID, run.ActionName, run.ActionType, run.Status)
		} else if prev.status != next.status {
			fmt.Fprintf(os.Stdout, "[action %s] session=%s name=%s type=%s %s → %s\n",
				ts, run.SessionID, run.ActionName, run.ActionType, prev.status, next.status)
		} else {
			fmt.Fprintf(os.Stdout, "[action %s] UPDATE session=%s name=%s type=%s status=%s\n",
				ts, run.SessionID, run.ActionName, run.ActionType, run.Status)
		}

		if previewText := truncateRunes(resultPlain, preview); previewText != "" {
			fmt.Fprintf(os.Stdout, "  result: %s\n", singleLine(previewText))
		} else if run.Status == types.Pending || run.Status == types.Running {
			payloadPlain := decryptField(encryptKey, run.Payload)
			if previewText := truncateRunes(payloadPlain, preview); previewText != "" {
				fmt.Fprintf(os.Stdout, "  payload: %s\n", singleLine(previewText))
			}
		}
		snaps[key] = next
	}
	return nil
}

func promptHumanInput(
	ctx context.Context,
	db *sqlite.Client,
	encryptKey *encrypt.EncryptKey,
	reader *bufio.Reader,
	seen map[string]struct{},
	notified map[string]struct{},
	sessionFilter string,
	interactive bool,
	logger *applog.Logger,
) error {
	runs, err := db.ListActionRunsByStatus(ctx, types.WaitingForHumanInput)
	if err != nil {
		return fmt.Errorf("list waiting human-input runs: %w", err)
	}

	// Drop memory for actions that left WAITING so the same action can ask again
	// (answer → Pending/Running → WAITING). Without this, seen/notified forever
	// suppress the second prompt and operators think watch must be restarted.
	waiting := make(map[string]struct{}, len(runs))
	for _, run := range runs {
		if sessionFilter != "" && run.SessionID != sessionFilter {
			continue
		}
		waiting[actionKey(run)] = struct{}{}
	}
	pruneHitlMemory(seen, waiting)
	pruneHitlMemory(notified, waiting)

	for _, run := range runs {
		if sessionFilter != "" && run.SessionID != sessionFilter {
			continue
		}
		key := actionKey(run)
		if _, done := seen[key]; done {
			continue
		}

		question := decryptField(encryptKey, run.Result)
		if question == "" {
			question = decryptField(encryptKey, run.Payload)
		}
		question = engine.PublicFacingText(question)

		if !interactive {
			if _, already := notified[key]; already {
				continue
			}
			fmt.Fprintf(os.Stdout, "\n[human-input WAITING] session=%s action=%s type=%s\n", run.SessionID, run.ActionName, run.ActionType)
			fmt.Fprintf(os.Stdout, "question: %s\n", singleLine(truncateRunes(question, 500)))
			fmt.Fprintln(os.Stdout, "answer via: SYMAGIOS_WATCH_HITL=1 ./scripts/compose.sh --profile auth --profile watch run --rm --no-deps watch")
			notified[key] = struct{}{}
			continue
		}

		fmt.Fprintf(os.Stdout, "\n[human-input] session=%s action=%s type=%s\n", run.SessionID, run.ActionName, run.ActionType)
		fmt.Fprintf(os.Stdout, "question: %s\n> ", question)

		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read operator input: %w", err)
		}

		input := strings.TrimSpace(line)
		if input == "" {
			fmt.Fprintln(os.Stderr, "empty input, skipping")
			continue
		}

		if err := engine.SubmitHumanInput(ctx, engine.HumanInputResponse{
			SessionID:  run.SessionID,
			ActionName: run.ActionName,
			ActionType: run.ActionType,
			Input:      input,
		}); err != nil {
			logger.Error(
				"failed to submit human input",
				append(applog.ActionFields(run.SessionID, run.ActionName, run.ActionType), "error", err)...,
			)
			continue
		}

		seen[key] = struct{}{}
		logger.Info(
			"human input submitted",
			append(applog.ActionFields(run.SessionID, run.ActionName, run.ActionType))...,
		)
	}
	return nil
}

func actionKey(run storage.ActionRun) string {
	return fmt.Sprintf("%s\x00%s\x00%s", run.SessionID, run.ActionName, run.ActionType)
}

func pruneHitlMemory(mem, waiting map[string]struct{}) {
	for k := range mem {
		if _, ok := waiting[k]; !ok {
			delete(mem, k)
		}
	}
}

func decryptField(encryptKey *encrypt.EncryptKey, raw string) string {
	if raw == "" {
		return ""
	}
	if encryptKey != nil {
		raw = encryptKey.DecryptStored(raw)
	}
	return engine.PublicFacingText(raw)
}

func singleLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", " ⏎ ")
	return strings.TrimSpace(s)
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}
