package llm

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	vertexAnthropicVersion = "vertex-2023-10-16"
	vertexCloudScope       = "https://www.googleapis.com/auth/cloud-platform"
)

// vertexRawPredictURL builds the Claude on Vertex :rawPredict endpoint.
// location "global" → https://aiplatform.googleapis.com/.../locations/global/...
// else → https://{location}-aiplatform.googleapis.com/.../locations/{location}/...
func vertexRawPredictURL(project, location, model string) (string, error) {
	project = strings.TrimSpace(project)
	location = strings.TrimSpace(location)
	model = strings.TrimSpace(model)
	if project == "" || location == "" || model == "" {
		return "", fmt.Errorf("llm: claude-vertex requires project, location, and model")
	}
	// Model id is a Vertex model name (may include @); do not PathEscape '@'.
	if strings.ContainsAny(model, "/?") {
		return "", fmt.Errorf("llm: invalid vertex model id %q", model)
	}
	path := fmt.Sprintf("/v1/projects/%s/locations/%s/publishers/anthropic/models/%s:rawPredict",
		url.PathEscape(project), url.PathEscape(location), model)
	var host string
	switch strings.ToLower(location) {
	case "global":
		host = "https://aiplatform.googleapis.com"
	case "us", "eu":
		// Multi-region REP endpoints.
		host = fmt.Sprintf("https://aiplatform.%s.rep.googleapis.com", strings.ToLower(location))
	default:
		host = fmt.Sprintf("https://%s-aiplatform.googleapis.com", location)
	}
	return host + path, nil
}

var (
	adcOnce   sync.Once
	adcSource oauth2.TokenSource
	adcErr    error
)

func vertexADCToken(ctx context.Context) (string, error) {
	adcOnce.Do(func() {
		creds, err := google.FindDefaultCredentials(ctx, vertexCloudScope)
		if err != nil {
			adcErr = err
			return
		}
		adcSource = creds.TokenSource
	})
	if adcErr != nil {
		return "", adcErr
	}
	if adcSource == nil {
		return "", fmt.Errorf("no ADC token source")
	}
	tok, err := adcSource.Token()
	if err != nil {
		return "", err
	}
	if tok == nil || tok.AccessToken == "" {
		return "", fmt.Errorf("empty ADC access token")
	}
	return tok.AccessToken, nil
}

// vertexBearerToken returns a Google OAuth access token for Vertex.
// Prefer Application Default Credentials (refreshing) so long-running engines do
// not depend on VERTEX_ACCESS_TOKEN (~1h) being re-exported on every recreate.
// Order: ADC → VERTEX_ACCESS_TOKEN / GOOGLE_OAUTH_ACCESS_TOKEN → cfg.APIKey (Vault).
// Set SYMAGIOS_VERTEX_FORCE_TOKEN=1 to skip ADC and use env/Vault only.
func vertexBearerToken(ctx context.Context, cfg Config) (string, error) {
	forceStatic := os.Getenv("SYMAGIOS_VERTEX_FORCE_TOKEN") == "1"

	if !forceStatic {
		if t, err := vertexADCToken(ctx); err == nil {
			return t, nil
		}
	}

	if t := firstNonEmpty(os.Getenv("VERTEX_ACCESS_TOKEN"), os.Getenv("GOOGLE_OAUTH_ACCESS_TOKEN")); t != "" {
		return t, nil
	}
	if t := strings.TrimSpace(cfg.APIKey); t != "" {
		return t, nil
	}

	if forceStatic {
		return "", fmt.Errorf("llm: claude-vertex: set VERTEX_ACCESS_TOKEN or GOOGLE_APPLICATION_CREDENTIALS (ADC skipped via SYMAGIOS_VERTEX_FORCE_TOKEN)")
	}
	// Surface ADC error if nothing else worked.
	if _, err := vertexADCToken(ctx); err != nil {
		return "", fmt.Errorf("llm: claude-vertex token: %w (run: gcloud auth application-default login)", err)
	}
	return "", fmt.Errorf("llm: claude-vertex: no token source")
}
