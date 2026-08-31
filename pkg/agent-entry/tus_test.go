package agententry

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"symagios/encrypt"
	"symagios/storage"
	"symagios/storage/sqlite"
	"symagios/utils/types"
)

func TestParseUploadMetadata(t *testing.T) {
	meta := parseUploadMetadata("agent_name b3JjaGVzdHJhdG9y,session_id c2Vzc2lvbi0x")
	if meta["agent_name"] != "orchestrator" {
		t.Fatalf("agent_name=%q", meta["agent_name"])
	}
	if meta["session_id"] != "session-1" {
		t.Fatalf("session_id=%q", meta["session_id"])
	}
}

func TestEncodeUploadMetadataRoundTrip(t *testing.T) {
	raw := encodeUploadMetadata(map[string]string{
		"agent_name": "demo",
		"session_id": "abc",
	})
	meta := parseUploadMetadata(raw)
	if meta["agent_name"] != "demo" || meta["session_id"] != "abc" {
		t.Fatalf("round trip failed: %#v", meta)
	}
}

func encodeUploadMetadata(m map[string]string) string {
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, k+" "+base64.StdEncoding.EncodeToString([]byte(v)))
	}
	return strings.Join(parts, ",")
}

func enablePlainMeshauth(t *testing.T) {
	t.Helper()
	t.Setenv("SYMAGIOS_MESHAUTH_PLAIN", "1")
}

func TestPostCreatesInMemoryUploadOnly(t *testing.T) {
	enablePlainMeshauth(t)
	db, err := sqlite.New(sqlite.Config{Path: filepath.Join(t.TempDir(), "post.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key, err := encrypt.NewEncryptKey(strings.Repeat("k", 32))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(db, key)

	req := httptest.NewRequest(http.MethodPost, "/agents", nil)
	req.Header.Set("X-User-ID", "tester")
	req.Header.Set("Upload-Length", "12")
	req.Header.Set("Upload-Metadata", encodeUploadMetadata(map[string]string{
		"session_id": "s-post",
		"agent_name": "orch",
	}))
	rr := httptest.NewRecorder()
	srv.handleAgentsCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	if _, err := db.GetActionRun(context.Background(), "s-post", "orch", types.Agent); err == nil {
		t.Fatal("POST must not create action_run")
	}
	up, err := srv.getPendingUpload("s-post", "orch")
	if err != nil {
		t.Fatal(err)
	}
	if up.offset != 0 || up.length != 12 {
		t.Fatalf("pending upload offset=%d length=%d", up.offset, up.length)
	}
	mf, err := db.GetWorkflowManifest(context.Background(), "s-post")
	if err != nil {
		t.Fatal(err)
	}
	if mf.Capabilities != "[]" {
		t.Fatalf("capabilities should stay empty until finalize: %s", mf.Capabilities)
	}
}

func TestPatchKeepsChunksInMemoryUntilFinalize(t *testing.T) {
	enablePlainMeshauth(t)
	db, err := sqlite.New(sqlite.Config{Path: filepath.Join(t.TempDir(), "u.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key, err := encrypt.NewEncryptKey(strings.Repeat("k", 32))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(db, key)
	ctx := context.Background()

	bodyObj := map[string]any{
		"name":         "orch",
		"data":         "hello world",
		"capabilities": []map[string]any{{"name": "triage", "type": "SKILL"}},
		"workflows":    []map[string]any{},
	}
	full, err := json.Marshal(bodyObj)
	if err != nil {
		t.Fatal(err)
	}

	_ = db.InsertWorkflowManifest(ctx, storage.WorkflowManifest{
		SessionID: "s1", Capabilities: "[]", WorkflowSteps: "[]",
	})
	srv.uploads[uploadKey("s1", "orch")] = &pendingUpload{
		sessionID: "s1",
		agentName: "orch",
		length:    int64(len(full)),
		offset:    0,
		buf:       nil,
	}

	patch := func(offset int64, body string) *http.Response {
		req := httptest.NewRequest(http.MethodPatch, "/agents/s1/orch", bytes.NewReader([]byte(body)))
		req.Header.Set("X-User-ID", "tester")
		req.Header.Set("Upload-Offset", strconv.FormatInt(offset, 10))
		req.Header.Set("Content-Type", "application/offset+octet-stream")
		req.SetPathValue("session_id", "s1")
		req.SetPathValue("agent_name", "orch")
		rr := httptest.NewRecorder()
		srv.handleAgentUploadPatch(rr, req)
		return rr.Result()
	}

	mid := len(full) / 2
	if resp := patch(0, string(full[:mid])); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("first patch: %d", resp.StatusCode)
	}
	if _, err := db.GetActionRun(ctx, "s1", "orch", types.Agent); err == nil {
		t.Fatal("expected no action_run before finalize")
	}
	if up, err := srv.getPendingUpload("s1", "orch"); err != nil || up.offset != int64(mid) {
		t.Fatalf("mid-upload pending offset want %d", mid)
	}

	if resp := patch(int64(mid), string(full[mid:])); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("second patch: %d", resp.StatusCode)
	}

	run, err := db.GetActionRun(ctx, "s1", "orch", types.Agent)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != types.Pending {
		t.Fatalf("want Pending after full upload, got %s", run.Status)
	}
	if run.Result != "" {
		t.Fatalf("result must stay empty, got %q", run.Result)
	}
	if plain := key.DecryptStored(run.Payload); plain != "hello world" {
		t.Fatalf("final payload=%q", plain)
	}
	if run.Payload == "hello world" {
		t.Fatal("payload should be encrypted when key is present")
	}

	mf, err := db.GetWorkflowManifest(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mf.Capabilities, "triage") {
		t.Fatalf("capabilities not updated: %s", mf.Capabilities)
	}
	if _, err := srv.getPendingUpload("s1", "orch"); err == nil {
		t.Fatal("pending upload should be cleared after successful finalize")
	}
}

func TestFinalizeFailureKeepsUploadRetryable(t *testing.T) {
	enablePlainMeshauth(t)
	db, err := sqlite.New(sqlite.Config{Path: filepath.Join(t.TempDir(), "f.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key, err := encrypt.NewEncryptKey(strings.Repeat("k", 32))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(db, key)
	ctx := context.Background()

	bad := []byte(`{"name":"orch","data":""}`) // missing usable data
	srv.uploads[uploadKey("s1", "orch")] = &pendingUpload{
		sessionID: "s1", agentName: "orch", length: int64(len(bad)),
	}

	req := httptest.NewRequest(http.MethodPatch, "/agents/s1/orch", bytes.NewReader(bad))
	req.Header.Set("X-User-ID", "tester")
	req.Header.Set("Upload-Offset", "0")
	req.Header.Set("Content-Type", "application/offset+octet-stream")
	req.SetPathValue("session_id", "s1")
	req.SetPathValue("agent_name", "orch")
	rr := httptest.NewRecorder()
	srv.handleAgentUploadPatch(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", rr.Code)
	}

	up, err := srv.getPendingUpload("s1", "orch")
	if err != nil {
		t.Fatal("upload should remain after finalize failure")
	}
	if up.offset != 0 {
		t.Fatalf("offset should rewind to 0 for retry, got %d", up.offset)
	}
	if _, err := db.GetActionRun(ctx, "s1", "orch", types.Agent); err == nil {
		t.Fatal("failed finalize must not create action_run")
	}

	// Valid retry with same offset.
	good, _ := json.Marshal(map[string]any{
		"name": "orch", "data": "agent body", "capabilities": []any{}, "workflows": []any{},
	})
	srv.uploads[uploadKey("s1", "orch")].length = int64(len(good))
	req2 := httptest.NewRequest(http.MethodPatch, "/agents/s1/orch", bytes.NewReader(good))
	req2.Header.Set("X-User-ID", "tester")
	req2.Header.Set("Upload-Offset", "0")
	req2.Header.Set("Content-Type", "application/offset+octet-stream")
	req2.SetPathValue("session_id", "s1")
	req2.SetPathValue("agent_name", "orch")
	rr2 := httptest.NewRecorder()
	srv.handleAgentUploadPatch(rr2, req2)
	if rr2.Code != http.StatusNoContent {
		t.Fatalf("retry want 204, got %d body=%s", rr2.Code, rr2.Body.String())
	}

	run, err := db.GetActionRun(ctx, "s1", "orch", types.Agent)
	if err != nil {
		t.Fatal(err)
	}
	if got := key.DecryptStored(run.Payload); got != "agent body" {
		t.Fatalf("payload=%q", got)
	}
	if _, err := srv.getPendingUpload("s1", "orch"); err == nil {
		t.Fatal("pending upload should be cleared after successful retry")
	}
}

func TestNilEncryptKeyStoresPlaintext(t *testing.T) {
	enablePlainMeshauth(t)
	db, err := sqlite.New(sqlite.Config{Path: filepath.Join(t.TempDir(), "plain.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv := New(db, nil)
	ctx := context.Background()

	body, _ := json.Marshal(map[string]any{
		"name": "orch", "data": "plain agent", "capabilities": []any{}, "workflows": []any{},
	})
	srv.uploads[uploadKey("s1", "orch")] = &pendingUpload{
		sessionID: "s1", agentName: "orch", length: int64(len(body)),
	}

	req := httptest.NewRequest(http.MethodPatch, "/agents/s1/orch", bytes.NewReader(body))
	req.Header.Set("X-User-ID", "tester")
	req.Header.Set("Upload-Offset", "0")
	req.Header.Set("Content-Type", "application/offset+octet-stream")
	req.SetPathValue("session_id", "s1")
	req.SetPathValue("agent_name", "orch")
	rr := httptest.NewRecorder()
	srv.handleAgentUploadPatch(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d body=%s", rr.Code, rr.Body.String())
	}

	run, err := db.GetActionRun(ctx, "s1", "orch", types.Agent)
	if err != nil {
		t.Fatal(err)
	}
	if run.Payload != "plain agent" {
		t.Fatalf("want plaintext payload, got %q", run.Payload)
	}
	if run.Result != "" {
		t.Fatalf("result must be empty, got %q", run.Result)
	}
}

func TestGetPendingUploadRequiresExactAgent(t *testing.T) {
	srv := New(nil, nil)
	srv.uploads[uploadKey("s1", "a")] = &pendingUpload{sessionID: "s1", agentName: "a", length: 3}
	srv.uploads[uploadKey("s1", "b")] = &pendingUpload{sessionID: "s1", agentName: "b", length: 3}

	if _, err := srv.getPendingUpload("s1", "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.getPendingUpload("s1", "missing"); err == nil {
		t.Fatal("expected miss")
	}
}

func TestPatchWithoutCreateReturns404(t *testing.T) {
	enablePlainMeshauth(t)
	srv := New(nil, nil)
	req := httptest.NewRequest(http.MethodPatch, "/agents/s1/orch", bytes.NewReader([]byte("x")))
	req.Header.Set("X-User-ID", "tester")
	req.Header.Set("Upload-Offset", "0")
	req.Header.Set("Content-Type", "application/offset+octet-stream")
	req.SetPathValue("session_id", "s1")
	req.SetPathValue("agent_name", "orch")
	rr := httptest.NewRecorder()
	srv.handleAgentUploadPatch(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rr.Code)
	}
}
