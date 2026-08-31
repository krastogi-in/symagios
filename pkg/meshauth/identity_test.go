package meshauth

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthorize_plainHTTP_requiresFlag(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-User-ID", "alice")
	if Authorize(req) {
		t.Fatal("plain HTTP without SYMAGIOS_MESHAUTH_PLAIN should deny")
	}
	t.Setenv("SYMAGIOS_MESHAUTH_PLAIN", "1")
	if !Authorize(req) {
		t.Fatal("plain HTTP with flag + X-User-ID should allow (tests)")
	}
}

func TestAuthorize_ingressNeedsJWT(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-User-ID", "alice")
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{
			{DNSNames: []string{"envoy-ingress.symagios.local"}},
		},
	}
	if Authorize(req) {
		t.Fatal("ingress without jwt payload should deny")
	}
	req.Header.Set("X-Jwt-Payload", "e30")
	if !Authorize(req) {
		t.Fatal("ingress + user + jwt payload header should allow")
	}
}

func TestAuthorize_peerAgentGatewayNoJWT(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-User-ID", "symagios-peer")
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{
			{DNSNames: []string{"agent-gateway.symagios.local"}},
		},
	}
	if !Authorize(req) {
		t.Fatal("peer agent-gateway should allow without JWT")
	}
}

func TestAuthorize_peerEnvoyEgressNoJWT(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-User-ID", "symagios-peer")
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{
			{DNSNames: []string{"envoy-egress"}},
		},
	}
	if !Authorize(req) {
		t.Fatal("envoy-egress should allow without JWT")
	}
}

func TestRequireA2AClients(t *testing.T) {
	cfg := &tls.Config{}
	RequireA2AClients(cfg)
	if cfg.VerifyPeerCertificate == nil {
		t.Fatal("expected VerifyPeerCertificate")
	}
}

func TestAuthorize_engineDenied(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-User-ID", "x")
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{
			{DNSNames: []string{"engine.symagios.local"}},
		},
	}
	if Authorize(req) {
		t.Fatal("engine cert should deny on agent-gateway")
	}
}

func TestPeerNameOK(t *testing.T) {
	cert := &x509.Certificate{DNSNames: []string{"envoy-ingress.symagios.local"}}
	if !PeerNameOK(cert, EnvoyIngress) {
		t.Fatal("expected SAN match")
	}
	cert = &x509.Certificate{Subject: pkix.Name{CommonName: "agent-gateway"}}
	if !PeerNameOK(cert, AgentGateway) {
		t.Fatal("expected CN match")
	}
}

func TestSourceFromCert(t *testing.T) {
	cert := &x509.Certificate{DNSNames: []string{"engine.symagios.local"}}
	if got := SourceFromCert(cert); got != "symagios-engine" {
		t.Fatalf("got %q", got)
	}
}
