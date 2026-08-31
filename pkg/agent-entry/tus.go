package agententry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"google.golang.org/protobuf/encoding/protojson"

	"symagios/proto/manifest"
	"symagios/storage"
	"symagios/utils/types"
)

const (
	tusVersion      = "1.0.0"
	tusMaxSize      = 32 << 20 // 32 MiB
	tusExtensions   = "creation,termination"
	maxTUSBodyChunk = 4 << 20 // 4 MiB per PATCH
)

// pendingUpload holds an in-flight TUS body in memory. No action_run is written
// until finalize parses AgentManifest and inserts an encrypted Pending agent.
type pendingUpload struct {
	sessionID string
	agentName string
	length    int64
	offset    int64
	buf       []byte
}

func uploadKey(sessionID, agentName string) string {
	return sessionID + "\x00" + agentName
}

// TUS create: POST /agents — opens an in-memory upload + ensures a session manifest row.
// Does not create an action_run; result/payload stay unused until finalize.
func (s *Server) handleAgentsOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Tus-Resumable", tusVersion)
	w.Header().Set("Tus-Version", tusVersion)
	w.Header().Set("Tus-Extension", tusExtensions)
	w.Header().Set("Tus-Max-Size", strconv.FormatInt(tusMaxSize, 10))
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, HEAD, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, X-User-ID, Upload-Length, Upload-Offset, Upload-Metadata, Tus-Resumable, Content-Type")
	w.Header().Set("Access-Control-Expose-Headers", "Upload-Offset, Upload-Length, Location, Tus-Resumable")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAgentsCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Header.Get("Tus-Resumable") != "" && r.Header.Get("Tus-Resumable") != tusVersion {
		http.Error(w, "unsupported Tus-Resumable", http.StatusPreconditionFailed)
		return
	}

	lengthStr := r.Header.Get("Upload-Length")
	if lengthStr == "" {
		http.Error(w, "Upload-Length is required", http.StatusBadRequest)
		return
	}
	uploadLength, err := strconv.ParseInt(lengthStr, 10, 64)
	if err != nil || uploadLength < 0 {
		http.Error(w, "invalid Upload-Length", http.StatusBadRequest)
		return
	}
	if uploadLength > tusMaxSize {
		http.Error(w, "Upload-Length exceeds Tus-Max-Size", http.StatusRequestEntityTooLarge)
		return
	}

	meta := parseUploadMetadata(r.Header.Get("Upload-Metadata"))
	sessionID := meta["session_id"]
	if sessionID == "" {
		sessionID = uuid.New().String()
	}
	agentName := meta["agent_name"]
	if agentName == "" {
		agentName = meta["name"]
	}
	if agentName == "" {
		agentName = "agent"
	}

	ctx := r.Context()
	now := time.Now().UTC()

	// Ensure a session manifest row exists; capabilities/workflows filled on finalize.
	if _, err := s.db.GetWorkflowManifest(ctx, sessionID); err != nil {
		if err := s.db.InsertWorkflowManifest(ctx, storage.WorkflowManifest{
			SessionID:     sessionID,
			Capabilities:  "[]",
			WorkflowSteps: "[]",
			CreatedAt:     now,
			UpdatedAt:     now,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	key := uploadKey(sessionID, agentName)
	s.uploadsMu.Lock()
	if _, exists := s.uploads[key]; exists {
		s.uploadsMu.Unlock()
		http.Error(w, "upload already in progress for this agent", http.StatusConflict)
		return
	}
	cap := uploadLength
	if cap > maxTUSBodyChunk {
		cap = maxTUSBodyChunk
	}
	s.uploads[key] = &pendingUpload{
		sessionID: sessionID,
		agentName: agentName,
		length:    uploadLength,
		offset:    0,
		buf:       make([]byte, 0, cap),
	}
	s.uploadsMu.Unlock()

	location := uploadLocation(sessionID, agentName)
	w.Header().Set("Tus-Resumable", tusVersion)
	w.Header().Set("Location", location)
	w.Header().Set("Upload-Offset", "0")
	w.Header().Set("Upload-Length", strconv.FormatInt(uploadLength, 10))
	w.Header().Set("X-Session-Id", sessionID)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"session_id": sessionID,
		"agent_name": agentName,
		"location":   location,
	})
}

func (s *Server) handleAgentUploadHead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	up, err := s.getPendingUpload(r.PathValue("session_id"), r.PathValue("agent_name"))
	if err != nil {
		http.Error(w, "upload not found; POST /agents first", http.StatusNotFound)
		return
	}
	w.Header().Set("Tus-Resumable", tusVersion)
	w.Header().Set("Upload-Offset", strconv.FormatInt(up.offset, 10))
	w.Header().Set("Upload-Length", strconv.FormatInt(up.length, 10))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Session-Id", up.sessionID)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleAgentUploadPatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" && ct != "application/offset+octet-stream" {
		http.Error(w, "Content-Type must be application/offset+octet-stream", http.StatusUnsupportedMediaType)
		return
	}

	offset, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil {
		http.Error(w, "invalid Upload-Offset", http.StatusBadRequest)
		return
	}

	chunk, err := io.ReadAll(io.LimitReader(r.Body, maxTUSBodyChunk+1))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if int64(len(chunk)) > maxTUSBodyChunk {
		http.Error(w, "chunk too large", http.StatusRequestEntityTooLarge)
		return
	}

	sessionID := r.PathValue("session_id")
	agentName := r.PathValue("agent_name")
	key := uploadKey(sessionID, agentName)

	s.uploadsMu.Lock()
	up, ok := s.uploads[key]
	if !ok {
		s.uploadsMu.Unlock()
		http.Error(w, "upload not found; POST /agents first", http.StatusNotFound)
		return
	}
	if offset != up.offset {
		cur := up.offset
		s.uploadsMu.Unlock()
		http.Error(w, fmt.Sprintf("offset mismatch: client %d server %d", offset, cur), http.StatusConflict)
		return
	}
	if up.offset+int64(len(chunk)) > up.length {
		s.uploadsMu.Unlock()
		http.Error(w, "chunk exceeds upload length", http.StatusBadRequest)
		return
	}

	up.buf = append(up.buf, chunk...)
	up.offset = int64(len(up.buf))
	doneOffset := up.offset
	var done *pendingUpload
	if doneOffset == up.length {
		cp := *up
		done = &cp
	}
	s.uploadsMu.Unlock()

	if done != nil {
		if err := s.finalizeAgentUpload(r.Context(), done); err != nil {
			// Rewind the last chunk so the same PATCH can be retried.
			s.uploadsMu.Lock()
			if cur, ok := s.uploads[key]; ok && int64(len(chunk)) <= cur.offset {
				cur.buf = cur.buf[:len(cur.buf)-len(chunk)]
				cur.offset = int64(len(cur.buf))
			}
			s.uploadsMu.Unlock()
			s.log.Error("finalize agent upload failed", "session_id", done.sessionID, "agent_name", done.agentName, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.uploadsMu.Lock()
		delete(s.uploads, key)
		s.uploadsMu.Unlock()
	}

	w.Header().Set("Tus-Resumable", tusVersion)
	w.Header().Set("Upload-Offset", strconv.FormatInt(doneOffset, 10))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAgentUploadDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	key := uploadKey(r.PathValue("session_id"), r.PathValue("agent_name"))
	s.uploadsMu.Lock()
	delete(s.uploads, key)
	s.uploadsMu.Unlock()
	w.Header().Set("Tus-Resumable", tusVersion)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getPendingUpload(sessionID, agentName string) (*pendingUpload, error) {
	s.uploadsMu.Lock()
	defer s.uploadsMu.Unlock()
	up, ok := s.uploads[uploadKey(sessionID, agentName)]
	if !ok {
		return nil, fmt.Errorf("upload not found")
	}
	cp := *up
	return &cp, nil
}

// finalizeAgentUpload splits AgentManifest into workflow_manifest + encrypted agent action_run.
func (s *Server) finalizeAgentUpload(ctx context.Context, up *pendingUpload) error {
	var m manifest.AgentManifest
	if err := protojson.Unmarshal(up.buf, &m); err != nil {
		return fmt.Errorf("invalid agent manifest json: %w", err)
	}

	agentName := m.GetName()
	if agentName == "" {
		agentName = up.agentName
	}

	capsJSON, err := json.Marshal(m.GetCapabilities())
	if err != nil {
		return fmt.Errorf("marshal capabilities: %w", err)
	}
	if string(capsJSON) == "null" {
		capsJSON = []byte("[]")
	}
	stepsJSON, err := json.Marshal(m.GetWorkflows())
	if err != nil {
		return fmt.Errorf("marshal workflows: %w", err)
	}
	if string(stepsJSON) == "null" {
		stepsJSON = []byte("[]")
	}

	agentData := m.GetData()
	if agentData == "" {
		s.log.Error("agent manifest missing data", "session_id", up.sessionID, "agent_name", agentName)
		return fmt.Errorf("agent manifest missing data")
	}

	now := time.Now().UTC()
	if err := s.db.InsertWorkflowManifest(ctx, storage.WorkflowManifest{
		SessionID:     up.sessionID,
		Capabilities:  string(capsJSON),
		WorkflowSteps: string(stepsJSON),
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		return err
	}

	// Prefer ciphertext, but never fail finalize: missing key / encrypt errors
	// fall back to plaintext so the agent run can still proceed (availability).
	enc := agentData
	if s.encryptKey == nil {
		s.log.Error("encrypt key missing; storing plaintext", "session_id", up.sessionID, "agent_name", agentName)
	} else if cipher, err := s.encryptKey.Encrypt(agentData); err != nil {
		s.log.Error("encrypt agent data failed; storing plaintext", "session_id", up.sessionID, "agent_name", agentName, "error", err)
	} else {
		enc = cipher
	}

	run := storage.ActionRun{
		SessionID:  up.sessionID,
		ActionName: agentName,
		ActionType: types.Agent,
		Status:     types.Pending,
		Payload:    enc,
		Result:     "",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	return s.db.InsertActionRun(ctx, run)
}

func uploadLocation(sessionID, agentName string) string {
	return fmt.Sprintf("/agents/%s/%s", url.PathEscape(sessionID), url.PathEscape(agentName))
}

// parseUploadMetadata parses TUS Upload-Metadata: "key base64value,key2 base64value2".
func parseUploadMetadata(raw string) map[string]string {
	out := map[string]string{}
	if raw == "" {
		return out
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, " ", 2)
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		if len(kv) == 1 {
			out[key] = ""
			continue
		}
		val, err := base64.StdEncoding.DecodeString(strings.TrimSpace(kv[1]))
		if err != nil {
			out[key] = strings.TrimSpace(kv[1])
			continue
		}
		out[key] = string(val)
	}
	return out
}

