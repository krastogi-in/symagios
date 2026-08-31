# mTLS between containers

Once traffic is past Envoy, we still care who is talking to whom. For this POC we use certificates from Vault’s PKI mount instead of SPIRE/SPIFFE (that can come later).

## Two different TLS hops on ingress Envoy

`deploy/envoy/envoy.yaml` is one process and one file, but two jobs:

1. **Client → Envoy** — Edge HTTPS. Envoy presents `envoy-ingress`’s server cert. Outside callers do not need a client cert.
2. **Envoy → agent-gateway / agent-entry** — East-west mTLS. Envoy presents a client cert and checks upstream SANs (`agent-gateway.symagios.local`, `agent-entry.symagios.local`).

There used to be a temptation to keep a second “envoy-mtls.yaml”. We dropped that; it was the same story twice.

## How certs get issued

`make mtls-issue` talks to Vault (dev mode on loopback inside the vault container), ensures the `pki` mount and `symagios-workload` role exist, then writes files under `deploy/certs/`:

```
deploy/certs/ca.crt
deploy/certs/ca.p12          # Keycloak truststore for client-auth
deploy/certs/<workload>/tls.crt
deploy/certs/<workload>/tls.key
```

Workloads we issue today include envoy-ingress, envoy-egress, agent-gateway, agent-entry, mcp-gateway, engine, search-broker, opa, keycloak, and vault. Names look like `agent-gateway.symagios.local`, and bare compose DNS names are usually included as SANs so either form works. The ingress cert also allows `localhost` and `127.0.0.1` so `curl https://localhost:8443` verifies cleanly with our CA.

## Special cases worth knowing

**Keycloak** listens HTTPS with client authentication required. Envoy’s JWKS cluster presents the envoy-ingress client cert. Local admin (`kcadm`) stays on unpublished HTTP inside the Keycloak container because kcadm cannot easily do mTLS.

**Vault** in `-dev` mode only speaks plain HTTP. We bind that to `127.0.0.1:8200` inside the vault container (Vault also takes cluster port `8201`). A tiny Envoy (`deploy/envoy/vault-mtls.yaml`, `network_mode: service:vault`) fronts mTLS on `:8202`. Other containers reach Vault at `https://vault.symagios.local:8202`. A real Vault TLS listener would replace that sidecar later.

**OPA’s ext_authz gRPC** is still plaintext on the compose network for now. That is an explicit POC tradeoff. Agent-gateway, agent-entry, mcp-gateway, and search-broker all listen TLS on `:8443` (mTLS). **agent-entry** requires `envoy-ingress` + JWT-derived headers from Envoy (Keycloak JWKS verify at the edge). **agent-gateway** allows `envoy-ingress` (same edge JWT path), and mesh peer certs `agent-gateway` / `envoy-egress` without JWT (synthetic `X-User-ID` for audit). OPA Decision API binds to loopback only; Envoy/OPA cert mounts are per-workload + CA (not the full PKI tree).

**Envoy egress** requires a Vault-PKI client cert (`require_client_certificate: true`). Port `:8082` is not published; only mesh workloads reach `:10001`. Lua overwrites `x-symagios-source` from the peer cert so OPA never trusts a spoofable header.

## Bringing certs online

```bash
make mtls-issue   # write/refresh deploy/certs
make mtls-up      # restart envoy with those mounts
```

If Keycloak or Envoy start before the cert files exist, they fail in confusing ways — issue certs first.
