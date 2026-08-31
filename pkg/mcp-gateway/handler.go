package mcpgateway

import (
	"encoding/json"
	"net/http"

	"symagios/meshauth"
)

// Handler exposes HTTP endpoints for the engine and local admin.
//
//	GET  /v1/mcp/tools          — attached catalog
//	POST /v1/mcp/tools/call     — call an attached tool
//	POST /v1/mcp/attach         — attach/replace a server (POC admin)
//	POST /v1/mcp/detach         — {"id":"..."} remove a server
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/v1/mcp/tools", h.handleTools)
	mux.HandleFunc("/v1/mcp/tools/call", h.handleCall)
	mux.HandleFunc("/v1/mcp/attach", h.handleAttach)
	mux.HandleFunc("/v1/mcp/detach", h.handleDetach)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func (h *Handler) handleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tools":   h.svc.Registry().ListTools(),
		"servers": h.svc.Registry().ListServers(),
	})
}

func (h *Handler) handleCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req CallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorBody{
			Code:      CodeToolNotFound,
			Message:   "invalid json: " + err.Error(),
			NeedHuman: true,
		})
		return
	}
	// Source is cert-bound when mTLS is present; ignore spoofable JSON body.
	if src := meshauth.SourceFromRequest(r); src != "" {
		req.Source = src
	} else if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		writeJSON(w, http.StatusForbidden, ErrorBody{
			Code:      CodeToolNotAllowed,
			Message:   "client certificate is not a recognized mesh source",
			NeedHuman: true,
		})
		return
	} else {
		// Plain HTTP (unit tests): drop body source so allowlists fail closed unless unset.
		req.Source = ""
	}
	resp, errBody := h.svc.Call(r.Context(), req)
	if errBody != nil {
		status := http.StatusBadRequest
		if errBody.Code == CodeUpstream || errBody.Code == CodeVault {
			status = http.StatusBadGateway
		}
		if errBody.Code == CodeContentBlocked {
			status = http.StatusForbidden
		}
		if errBody.NeedHuman {
			status = http.StatusConflict // signal engine: pause for human
		}
		writeJSON(w, status, errBody)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleAttach(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !AdminAuthorized(r) {
		http.Error(w, "admin token required", http.StatusUnauthorized)
		return
	}
	var spec ServerSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := h.svc.Registry().Attach(spec); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "attached", "id": spec.ID})
}

func (h *Handler) handleDetach(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !AdminAuthorized(r) {
		http.Error(w, "admin token required", http.StatusUnauthorized)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	if !h.svc.Registry().Detach(body.ID) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "detached", "id": body.ID})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
