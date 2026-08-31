package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrLanguageUnavailable means the script runtime is not installed on this host.
var ErrLanguageUnavailable = errors.New("language not available")

// ScriptRequest is the decrypted SCRIPT action payload.
type ScriptRequest struct {
	Language   string   `json:"language"`
	Code       string   `json:"code"`
	Args       []string `json:"args,omitempty"`
	TimeoutSec int      `json:"timeout_sec,omitempty"`
	WorkDir    string   `json:"workdir,omitempty"`
}

// ScriptResult is stdout/stderr from a sandboxed script run.
type ScriptResult struct {
	Language            string `json:"language"`
	Interpreter         string `json:"interpreter,omitempty"`
	ExitCode            int    `json:"exit_code"`
	Stdout              string `json:"stdout"`
	Stderr              string `json:"stderr"`
	LanguageUnavailable bool   `json:"language_unavailable,omitempty"`
	Message             string `json:"message,omitempty"`
}

// known runtimes → candidate binaries (first LookPath wins).
var scriptRuntimes = map[string][]string{
	"python":     {"python3", "python"},
	"python3":    {"python3", "python"},
	"py":         {"python3", "python"},
	"bash":       {"bash"},
	"sh":         {"sh", "bash"},
	"shell":      {"bash", "sh"},
	"node":       {"node"},
	"javascript": {"node"},
	"js":         {"node"},
	"ruby":       {"ruby"},
	"perl":       {"perl"},
	"php":        {"php"},
	"deno":       {"deno"},
	"lua":        {"lua"},
	"r":          {"Rscript"},
	"rscript":    {"Rscript"},
}

func resolveInterpreter(language string) (bin string, err error) {
	lang := strings.ToLower(strings.TrimSpace(language))
	if lang == "" {
		return "", fmt.Errorf("%w: language field is required", ErrLanguageUnavailable)
	}
	cands, ok := scriptRuntimes[lang]
	if !ok {
		// Allow an explicit absolute path or bare binary name as language.
		if path, e := exec.LookPath(lang); e == nil {
			return path, nil
		}
		return "", fmt.Errorf("%w: %s", ErrLanguageUnavailable, lang)
	}
	for _, c := range cands {
		if path, e := exec.LookPath(c); e == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("%w: %s (tried %s)", ErrLanguageUnavailable, lang, strings.Join(cands, ", "))
}

func scriptExt(language string) string {
	switch strings.ToLower(language) {
	case "python", "python3", "py":
		return ".py"
	case "node", "javascript", "js":
		return ".js"
	case "ruby":
		return ".rb"
	case "perl":
		return ".pl"
	case "php":
		return ".php"
	case "lua":
		return ".lua"
	case "r", "rscript":
		return ".R"
	default:
		return ".sh"
	}
}

// RunScript executes inline code with the resolved interpreter.
// If the language runtime is missing, LanguageUnavailable is set (no process started).
func RunScript(ctx context.Context, req ScriptRequest) ScriptResult {
	lang := strings.TrimSpace(req.Language)
	out := ScriptResult{Language: lang}

	bin, err := resolveInterpreter(lang)
	if err != nil {
		out.LanguageUnavailable = true
		out.Message = err.Error()
		out.ExitCode = 127
		return out
	}
	out.Interpreter = bin

	if strings.TrimSpace(req.Code) == "" {
		out.Message = "script code is empty"
		out.ExitCode = 1
		return out
	}

	timeout := time.Duration(req.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workDir := req.WorkDir
	if workDir == "" {
		workDir = os.Getenv("SYMAGIOS_SCRIPT_WORKDIR")
	}
	if workDir == "" {
		workDir = "/tmp/symagios-workspace"
	}
	_ = os.MkdirAll(workDir, 0o700)

	tmp, err := os.MkdirTemp(workDir, "script-*")
	if err != nil {
		out.Message = err.Error()
		out.ExitCode = 1
		return out
	}
	defer os.RemoveAll(tmp)

	scriptPath := filepath.Join(tmp, "main"+scriptExt(lang))
	if err := os.WriteFile(scriptPath, []byte(req.Code), 0o600); err != nil {
		out.Message = err.Error()
		out.ExitCode = 1
		return out
	}

	args := append([]string{scriptPath}, req.Args...)
	cmd, err := newScriptCmd(runCtx, bin, args, workDir, tmp)
	if err != nil {
		out.Message = err.Error()
		out.ExitCode = 1
		return out
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	out.Stdout = stdout.String()
	out.Stderr = stderr.String()
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			out.Message = "script timed out"
			out.ExitCode = 124
			return out
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			out.ExitCode = ee.ExitCode()
		} else {
			out.Message = err.Error()
			out.ExitCode = 1
		}
		return out
	}
	out.ExitCode = 0
	return out
}

func parseScriptRequest(payload string) (ScriptRequest, error) {
	var req ScriptRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		// Treat raw payload as bash if it doesn't look like JSON.
		trim := strings.TrimSpace(payload)
		if strings.HasPrefix(trim, "{") {
			return req, fmt.Errorf("invalid script json: %w", err)
		}
		return ScriptRequest{Language: "bash", Code: payload}, nil
	}
	if req.Language == "" {
		req.Language = "bash"
	}
	return req, nil
}

func scriptResultJSON(res ScriptResult) string {
	b, _ := json.Marshal(res)
	return string(b)
}
