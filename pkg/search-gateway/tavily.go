package searchgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"symagios/egress"
)

const tavilySearchPath = "/search"

type tavilyRequest struct {
	APIKey      string `json:"api_key"`
	Query       string `json:"query"`
	SearchDepth string `json:"search_depth"`
}

// Result is a single governed search hit formatted for LLM tool output.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
	Score   float64 `json:"score,omitempty"`
}

type tavilyResponse struct {
	Results []struct {
		Title   string  `json:"title"`
		URL     string  `json:"url"`
		Content string  `json:"content"`
		Score   float64 `json:"score"`
	} `json:"results"`
	Answer string `json:"answer"`
}

// Client performs Tavily calls through the Symagios egress gateway.
type Client struct {
	cfg        Config
	httpClient *http.Client
	apiKey     string
}

func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	apiKey, err := resolveAPIKey(ctx, cfg)
	if err != nil {
		return nil, err
	}
	httpClient, err := newHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{
		cfg:        cfg,
		apiKey:     apiKey,
		httpClient: httpClient,
	}, nil
}

// Search is the single choke point for governed web search.
func (c *Client) Search(ctx context.Context, query string) ([]Result, string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, "", fmt.Errorf("query is required")
	}

	body, err := json.Marshal(tavilyRequest{
		APIKey:      c.apiKey,
		Query:       query,
		SearchDepth: "basic",
	})
	if err != nil {
		return nil, "", err
	}

	egressURL := "https://" + c.cfg.TavilyHost + tavilySearchPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, egressURL, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := egress.SetSource(req, c.cfg.SourceService); err != nil {
		return nil, "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("tavily search: %s: %s", resp.Status, string(respBody))
	}

	var parsed tavilyResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, "", err
	}

	results := make([]Result, 0, len(parsed.Results))
	for _, hit := range parsed.Results {
		results = append(results, Result{
			Title:   hit.Title,
			URL:     hit.URL,
			Content: hit.Content,
			Score:   hit.Score,
		})
	}
	return results, parsed.Answer, nil
}

// FormatForTool renders search output as plain text for LLM tool_result content.
func FormatForTool(results []Result, answer string) string {
	if answer != "" {
		return answer
	}
	var b strings.Builder
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "Title: %s\nURL: %s\n%s", r.Title, r.URL, r.Content)
	}
	return b.String()
}
