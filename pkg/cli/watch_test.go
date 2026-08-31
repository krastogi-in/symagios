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
