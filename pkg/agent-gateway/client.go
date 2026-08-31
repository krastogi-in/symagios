package agentgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"symagios/proto/a2a"
)

// Client is an HTTP client for A2A gateway endpoints.
type Client struct {
	baseURL       string
	userID        string
	httpClient    *http.Client
	forwardHost   string
	sourceService string
}

func NewClient(baseURL, userID string) *Client {
	return newClient(baseURL, userID, &http.Client{Timeout: 30 * time.Second})
}

func newClient(baseURL, userID string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		userID:     userID,
		httpClient: httpClient,
	}
}

func hostBaseURL(host string) string {
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return strings.TrimRight(host, "/")
	}
	return "http://" + host
}

func (g *AgentGateway) remoteClient(host string) *Client {
	userID := fmt.Sprintf("symagios-%s", host)
	if g.config.EgressProxy != "" {
		return newEgressClient(g.config.EgressProxy, host, userID, g.client)
	}
	return newClient(hostBaseURL(host), userID, g.client)
}

func newEgressClient(egressProxy, targetHost, userID string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:       strings.TrimRight(egressProxy, "/"),
		userID:        userID,
		httpClient:    httpClient,
		forwardHost:   hostAuthority(hostBaseURL(targetHost)),
		sourceService: "agent-gateway",
	}
}

func hostAuthority(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return strings.TrimPrefix(strings.TrimPrefix(baseURL, "https://"), "http://")
	}
	return parsed.Host
}

func (g *AgentGateway) retrieveExternalAgentCard(ctx context.Context, host string, updateAgentCard bool) error {
	if host == "" {
		return fmt.Errorf("retrieve agent card: empty host")
	}
	if !updateAgentCard {
		if _, ok := g.agentCards.Get(host); ok {
			return nil
		}
	}
	card, err := g.remoteClient(host).GetAgentCard(ctx)
	if err != nil {
		return err
	}
	g.agentCards.Add(host, card)
	// Replace mappings for this host so late-uploaded capabilities appear and
	// removed ones do not linger.
	g.hostRegistry.ClearHost(host)
	g.hostRegistry.IndexCard(host, card)
	return nil
}

func (c *Client) GetAgentCard(ctx context.Context) (*a2a.AgentCard, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/.well-known/agent-card.json", nil)
	if err != nil {
		return nil, err
	}
	c.prepareRequest(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get agent card: %s", resp.Status)
	}

	var card a2a.AgentCard
	if err := json.Unmarshal(body, &card); err != nil {
		return nil, err
	}
	return &card, nil
}

func (c *Client) SendMessage(ctx context.Context, message *a2a.Message) (*a2a.Task, error) {
	body, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/message/send", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.prepareRequest(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("send message: %s: %s", resp.Status, string(responseBody))
	}

	var task a2a.Task
	if err := json.Unmarshal(responseBody, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (c *Client) GetTask(ctx context.Context, taskID string) (*a2a.Task, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/tasks/"+taskID, nil)
	if err != nil {
		return nil, err
	}
	c.prepareRequest(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get task: %s: %s", resp.Status, string(body))
	}

	var task a2a.Task
	if err := json.Unmarshal(body, &task); err != nil {
		var message a2a.Message
		if err := json.Unmarshal(body, &message); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("no task returned: %w", err)
	}
	return &task, nil
}

func (c *Client) ListTasks(ctx context.Context, contextID string) ([]*a2a.Task, error) {
	url := fmt.Sprintf("%s/tasks/list?contextId=%s", c.baseURL, contextID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c.prepareRequest(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list tasks: %s: %s", resp.Status, string(body))
	}

	var tasks []*a2a.Task
	if err := json.Unmarshal(body, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (c *Client) CancelTask(ctx context.Context, taskID string) (*a2a.Task, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/tasks/"+taskID+"/cancel", nil)
	if err != nil {
		return nil, err
	}
	c.prepareRequest(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cancel task: %s: %s", resp.Status, string(body))
	}

	var task a2a.Task
	if err := json.Unmarshal(body, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (c *Client) prepareRequest(req *http.Request) {
	if c.forwardHost != "" {
		req.Host = c.forwardHost
		req.Header.Set("Host", c.forwardHost)
	}
	if c.sourceService != "" {
		req.Header.Set("X-Symagios-Source", c.sourceService)
	}
	if c.userID != "" {
		req.Header.Set("X-User-ID", c.userID)
	}
}
