package engine_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"symagios/engine"
)

func TestRunScriptBash(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	res := engine.RunScript(context.Background(), engine.ScriptRequest{
		Language: "bash",
		Code:     "echo hello-from-script",
	})
	if res.LanguageUnavailable {
		t.Fatalf("%+v", res)
	}
	if res.ExitCode != 0 {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(res.Stdout, "hello-from-script") {
		t.Fatalf("stdout=%q", res.Stdout)
	}
}

func TestRunScriptLanguageUnavailable(t *testing.T) {
	res := engine.RunScript(context.Background(), engine.ScriptRequest{
		Language: "definitely-not-a-real-lang-xyz",
		Code:     "print(1)",
	})
	if !res.LanguageUnavailable {
		t.Fatalf("expected language_unavailable: %+v", res)
	}
	if !strings.Contains(res.Message, "language not available") {
		t.Fatalf("message=%q", res.Message)
	}
	if res.ExitCode != 127 {
		t.Fatalf("exit=%d", res.ExitCode)
	}
}

func TestRunScriptPythonMissingOrRuns(t *testing.T) {
	res := engine.RunScript(context.Background(), engine.ScriptRequest{
		Language: "python3",
		Code:     "print('py-ok')",
	})
	if res.LanguageUnavailable {
		if !strings.Contains(res.Message, "language not available") {
			t.Fatalf("%q", res.Message)
		}
		return
	}
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "py-ok") {
		t.Fatalf("%+v", res)
	}
}

func TestScriptRequestJSONRoundTripShape(t *testing.T) {
	raw := `{"language":"bash","code":"echo hi","timeout_sec":5}`
	var req engine.ScriptRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	if req.Language != "bash" || req.Code != "echo hi" {
		t.Fatalf("%+v", req)
	}
}
