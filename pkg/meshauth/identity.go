package meshauth

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// Workload SAN basenames used on the east-west mesh.
const (
	EnvoyIngress = "envoy-ingress"
	EnvoyEgress  = "envoy-egress"
	AgentGateway = "agent-gateway"
	Engine       = "engine"
)

// Authorize checks caller identity for agent-gateway / agent-entry.
//
// - Plain HTTP: only when SYMAGIOS_MESHAUTH_PLAIN=1 (unit tests).
// - envoy-ingress: human edge path — Envoy jwt_authn already verified the
//   Bearer token with Keycloak JWKS; we only require the derived headers
//   (X-User-ID + X-Jwt-Payload) that Envoy sets after that check.
// - agent-gateway / envoy-egress: mesh peer hop — cert identity is enough
//   (peers set a synthetic X-User-ID for audit; no JWT).
func Authorize(r *http.Request) bool {
	userID := strings.TrimSpace(r.Header.Get("X-User-ID"))
	if userID == "" {
		return false
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return plainHTTPAllowed()
	}
	cert := r.TLS.PeerCertificates[0]
	if PeerNameOK(cert, EnvoyIngress) {
		return strings.TrimSpace(r.Header.Get("X-Jwt-Payload")) != ""
	}
	if PeerNameOK(cert, AgentGateway) || PeerNameOK(cert, EnvoyEgress) {
		return true
	}
	return false
}

func plainHTTPAllowed() bool {
	v := strings.TrimSpace(os.Getenv("SYMAGIOS_MESHAUTH_PLAIN"))
	return v == "1" || strings.EqualFold(v, "true")
}

// SourceFromCert maps a workload client cert to the OPA / allowlist source string.
// engine → symagios-engine (matches egress Lua); other known workloads keep their basename.
func SourceFromCert(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	for _, n := range cert.DNSNames {
		if src := sourceFromWorkloadName(n); src != "" {
			return src
		}
	}
	return sourceFromWorkloadName(cert.Subject.CommonName)
}

func sourceFromWorkloadName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimSuffix(name, ".symagios.local")
	switch name {
	case "", EnvoyIngress, EnvoyEgress:
		return ""
	case Engine, "search-broker":
		return "symagios-engine"
	default:
		return name
	}
}

// SourceFromRequest returns cert-bound source when mTLS peer certs are present.
func SourceFromRequest(r *http.Request) string {
	if r == nil || r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return ""
	}
	return SourceFromCert(r.TLS.PeerCertificates[0])
}

// PeerNameOK is true when cert DNS SAN or CN matches want (bare or .symagios.local).
func PeerNameOK(cert *x509.Certificate, want string) bool {
	if cert == nil {
		return false
	}
	want = strings.ToLower(strings.TrimSpace(want))
	for _, n := range cert.DNSNames {
		if matchWorkload(n, want) {
			return true
		}
	}
	return matchWorkload(cert.Subject.CommonName, want)
}

func matchWorkload(name, want string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || want == "" {
		return false
	}
	if name == want {
		return true
	}
	return strings.TrimSuffix(name, ".symagios.local") == want
}

// RequireClients rejects client certs whose SAN/CN is not in allowed basenames.
func RequireClients(cfg *tls.Config, allowed ...string) {
	if cfg == nil || len(allowed) == 0 {
		return
	}
	names := append([]string{}, allowed...)
	cfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("meshauth: missing client certificate")
		}
		cert, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("meshauth: parse client cert: %w", err)
		}
		for _, want := range names {
			if PeerNameOK(cert, want) {
				return nil
			}
		}
		return fmt.Errorf("meshauth: client SAN not in allowlist %v", names)
	}
}

// RequireEnvoyIngressClient allows only the edge sidecar (agent-entry, etc.).
func RequireEnvoyIngressClient(cfg *tls.Config) {
	RequireClients(cfg, EnvoyIngress)
}

// RequireA2AClients allows ingress (humans) and mesh peer hops to agent-gateway.
func RequireA2AClients(cfg *tls.Config) {
	RequireClients(cfg, EnvoyIngress, AgentGateway, EnvoyEgress)
}

// RequireEngineClient allows only the engine workload (search-broker, mcp-gateway).
func RequireEngineClient(cfg *tls.Config) {
	RequireClients(cfg, Engine)
}
