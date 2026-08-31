# Mesh hardening

What we tightened for the secured orchestration POC:

## Network exposure

- **OPA gRPC `:9191`** is reachable on the compose network for Envoy ext_authz. **Decision API `:8181`** binds to `127.0.0.1` inside the opa container only (not mesh-reachable, not host-published). Use `./scripts/compose.sh --profile auth exec opa wget -qO- http://127.0.0.1:8181/health` for local debug.
- Host bind mounts use `:ro,z` so rootless Podman on SELinux (Fedora) can read policies/certs. Docker ignores the label when SELinux is off.
- Attach/detach on **mcp-gateway** requires `X-Symagios-Admin-Token` (or loopback-only when the token env is unset).

## Optional OPA ↔ Envoy TLS

Laptop default is still plaintext gRPC on the compose bridge (same trust domain as other internal hops). To enable TLS:

1. Issue an `opa` cert via `scripts/issue-mtls-certs.sh` (include SAN `opa` / `opa.symagios.local`).
2. Run OPA with TLS flags, for example:

```text
--tls-cert-file=/certs/opa/tls.crt
--tls-private-key-file=/certs/opa/tls.key
--tls-ca-cert-file=/certs/ca.crt
```

3. Add an `UpstreamTlsContext` on the Envoy `opa_ext_authz` cluster (client cert + CA). See the comment in `deploy/envoy/envoy.yaml`.

## Policy depth

Ingress (`deploy/opa/policies/envoy/ingress.rego`) now checks:

| Capability | Roles (`x-symagios-roles`) |
|------------|----------------------------|
| `/message/send` | operator, agent-runner, admin |
| `POST`/`DELETE` `/peers` | operator, admin |
| TUS agent write | operator, admin |
| MCP `tools/call` | agent-runner, operator, admin |
| MCP attach/detach | admin |

Roles are **fail-closed**: empty or missing `x-symagios-roles` means `has_role` never matches. Envoy JWT authn forwards the payload; a Lua filter after JWT flattens `realm_access.roles` into `x-symagios-roles` (comma-separated). Keycloak realm roles `admin` / `operator` / `agent-runner` are defined in `deploy/keycloak/symagios-realm.json`; the bootstrap operator user gets `operator,agent-runner` from `.env` / Vault.

Egress (`egress.rego`) allowlists:

- search → `api.tavily.com` (source `symagios-engine`)
- LLM → `api.anthropic.com`, `api.openai.com`, Vertex `*aiplatform*.googleapis.com` (source `symagios-engine`, via `SYMAGIOS_EGRESS_PROXY`)
- MCP upstream → localhost/127.0.0.1 for stubs; extend hosts as you attach real MCP servers (source `mcp-gateway`, via `SYMAGIOS_EGRESS_PROXY`)

Engine, search-broker, mcp-gateway, and agent-gateway dial Envoy egress (`https://envoy-egress:10001`) with workload mTLS instead of opening plain `http.Client` sockets to the public internet. Egress is mesh-only (not published) and derives caller identity from the client cert.

## Peer A2A

Peer hops to **agent-gateway** use workload mTLS (`agent-gateway` / `envoy-egress` client certs) with a synthetic `X-User-ID` — no JWT. Human edge traffic still goes `envoy-ingress` + JWT. Message `Parts` carry store ciphertext (shared Vault key); the receiver upserts `Pending` only when the session already exists (no A2A manifest create). Runtime peer seed: `POST`/`DELETE` `/peers` (operator/admin) or `SYMAGIOS_AGENT_GATEWAY_EXTERNAL_HOSTS`.
