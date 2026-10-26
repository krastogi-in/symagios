package cli

import "testing"

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("hello", 10); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if got := truncateRunes("abcdefghij", 5); got != "abcde…" {
		t.Fatalf("got %q", got)
	}
	if got := truncateRunes("日本語テスト", 3); got != "日本語…" {
		t.Fatalf("got %q", got)
	}
}

func TestStdinIsInteractiveDoesNotPanic(t *testing.T) {
	// Just ensure the helper runs; CI stdin is usually not a TTY.
	_ = stdinIsInteractive()
}

func TestSingleLine(t *testing.T) {
	if got := singleLine("a\nb\nc"); got != "a ⏎ b ⏎ c" {
		t.Fatalf("got %q", got)
	}
}

func TestPruneHitlMemoryAllowsSecondAsk(t *testing.T) {
	seen := map[string]struct{}{"sess\x00guard-hitl\x00AGENT": {}}
	// After SubmitHumanInput the run is Pending — not in waiting set.
	pruneHitlMemory(seen, map[string]struct{}{})
	if len(seen) != 0 {
		t.Fatalf("expected seen cleared when action left WAITING, got %#v", seen)
	}
	// Same action parks again → empty seen → prompt/notify again.
	waiting := map[string]struct{}{"sess\x00guard-hitl\x00AGENT": {}}
	pruneHitlMemory(seen, waiting)
	if _, done := seen["sess\x00guard-hitl\x00AGENT"]; done {
		t.Fatal("should not re-mark as seen without an answer")
	}
}
