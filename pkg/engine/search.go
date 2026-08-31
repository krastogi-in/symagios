package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// SearchRequest is the decrypted WEB_SEARCH action payload.
type SearchRequest struct {
	Query string `json:"query"`
}

// SearchOutcome is the engine-facing result of governed search.
type SearchOutcome struct {
	Text        string `json:"text"`
	Query       string `json:"query,omitempty"`
	Answer      string `json:"answer,omitempty"`
	Message     string `json:"message,omitempty"`
	Unavailable bool   `json:"unavailable,omitempty"`
}

var (
	searchClientOnce sync.Once
	searchHTTPClient *http.Client
)

func searchBrokerURL() string {
	base := os.Getenv("SYMAGIOS_SEARCH_BROKER")
	if base != "" {
		return strings.TrimRight(base, "/")
	}
	return "https://search-broker:8443/v1/search"
}

func searchClient() *http.Client {
	searchClientOnce.Do(func() {
		searchHTTPClient = &http.Client{Timeout: 60 * time.Second}
		cert := envFirst("SYMAGIOS_SEARCH_CLIENT_CERT", "VAULT_CLIENT_CERT")
		key := envFirst("SYMAGIOS_SEARCH_CLIENT_KEY", "VAULT_CLIENT_KEY")
		ca := envFirst("SYMAGIOS_SEARCH_CA", "VAULT_CACERT")
		if cert == "" || key == "" {
			return
		}
		tlsCfg, err := loadMTLSClient(cert, key, ca)
		if err != nil {
			executeLogger.Error("search client tls config failed; using plain HTTP client", "error", err)
			return
		}
		searchHTTPClient.Transport = &http.Transport{TLSClientConfig: tlsCfg}
	})
	return searchHTTPClient
}

func parseSearchRequest(payload string) (SearchRequest, error) {
	var req SearchRequest
	trim := strings.TrimSpace(payload)
	if trim == "" {
		return req, fmt.Errorf("search query is required")
	}
	if err := json.Unmarshal([]byte(trim), &req); err != nil {
		// Allow raw string payload as the query.
		if strings.HasPrefix(trim, "{") {
			return req, fmt.Errorf("invalid search json: %w", err)
		}
		req.Query = trim
	}
	if req.Query == "" {
		return req, fmt.Errorf("search query is required")
	}
	return req, nil
}

// CallSearchBroker POSTs to the governed search-broker (mTLS → broker → egress → Tavily).
// If the broker is unreachable or misconfigured, Unavailable is set.
func CallSearchBroker(ctx context.Context, query string) SearchOutcome {
	url := searchBrokerURL()
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchOutcome{Unavailable: true, Message: "search not available: empty query"}
	}

	body, _ := json.Marshal(map[string]string{"query": query})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return SearchOutcome{Unavailable: true, Message: "search not available: " + err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := searchClient().Do(req)
	if err != nil {
		return SearchOutcome{Unavailable: true, Message: "search not available: " + err.Error(), Query: query}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return SearchOutcome{Unavailable: true, Message: "search not available: " + err.Error(), Query: query}
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = resp.Status
		}
		return SearchOutcome{
			Unavailable: true,
			Query:       query,
			Message:     "search not available: " + msg,
		}
	}

	var parsed struct {
		Query  string `json:"query"`
		Answer string `json:"answer"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return SearchOutcome{Unavailable: true, Message: "search not available: bad response", Query: query}
	}
	text := parsed.Text
	if text == "" {
		text = string(raw)
	}
	return SearchOutcome{
		Text:   text,
		Query:  parsed.Query,
		Answer: parsed.Answer,
	}
}

func searchResultJSON(out SearchOutcome) string {
	b, _ := json.Marshal(out)
	return string(b)
}
