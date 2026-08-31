package egress_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"symagios/egress"
)

func TestSetSource(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := egress.SetSource(req, "symagios-engine"); err != nil {
		t.Fatal(err)
	}
	if req.URL.Host != "api.anthropic.com" {
		t.Fatalf("host=%s", req.URL.Host)
	}
	if req.Header.Get("X-Symagios-Source") != "symagios-engine" {
		t.Fatalf("source=%q", req.Header.Get("X-Symagios-Source"))
	}
}

func TestSetSourceKeepsUpstreamURL(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/chat/completions?x=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := egress.SetSource(req, "symagios-engine"); err != nil {
		t.Fatal(err)
	}
	if req.URL.Scheme != "https" || req.URL.Host != "api.openai.com" {
		t.Fatalf("url=%s", req.URL.String())
	}
}

func TestHTTPClientSetsProxy(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()

	client, err := egress.Config{Proxy: proxy.URL}.HTTPClient(0)
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := client.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		t.Fatal("expected Transport.Proxy to be set")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	u, err := tr.Proxy(req)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := url.Parse(proxy.URL)
	if u.String() != want.String() {
		t.Fatalf("proxy=%s want=%s", u, want)
	}
}
