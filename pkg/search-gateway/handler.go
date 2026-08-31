package searchgateway

import (
	"encoding/json"
	"net/http"
)

type searchRequest struct {
	Query string `json:"query"`
}

type searchResponse struct {
	Query   string   `json:"query"`
	Answer  string   `json:"answer,omitempty"`
	Results []Result `json:"results"`
	Text    string   `json:"text"`
}

// Handler exposes governed search over HTTP for engines and examples.
type Handler struct {
	client *Client
}

func NewHandler(client *Client) *Handler {
	return &Handler{client: client}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	results, answer, err := h.client.Search(r.Context(), req.Query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	resp := searchResponse{
		Query:   req.Query,
		Answer:  answer,
		Results: results,
		Text:    FormatForTool(results, answer),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
