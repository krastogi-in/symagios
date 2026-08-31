# Secured MCP gateway

Agents never call MCP servers directly. They ask **mcp-gateway**, which only runs tools that are **attached** in `deploy/mcp/registry.json`. Credentials come from Vault when a real upstream is configured. Missing tools return an error with `need_human: true` so the engine parks the run in `WAITING_FOR_HUMAN_INPUT`.

East-west traffic is **mTLS**: mcp-gateway listens TLS on `:8443` and requires a client certificate (engine presents its Vault PKI cert).

Upstream `http` / `sse` calls go through **Envoy egress** (`SYMAGIOS_EGRESS_PROXY`, source `mcp-gateway`). Stub and stdio stay local.

## Transports

| `transport` | When to use |
|-------------|-------------|
| `stub` | Local POC, no network |
| `http` | Streamable HTTP / JSON-RPC `POST {base_url}/mcp` |
| `sse` | SSE servers; set `messages_url` for the POST endpoint |
| `stdio` | Local process: `command` + `args` (+ optional Vault token in `MCP_AUTH_TOKEN`) |

Attach/detach require `X-Symagios-Admin-Token` (`SYMAGIOS_MCP_ADMIN_TOKEN`) or loopback.

## Run locally

```bash
# With mTLS certs from make mtls-issue:
export SYMAGIOS_MCP_TLS_CERT=deploy/certs/mcp-gateway/tls.crt
export SYMAGIOS_MCP_TLS_KEY=deploy/certs/mcp-gateway/tls.key
export SYMAGIOS_MCP_TLS_CA=deploy/certs/ca.crt
make mcp-gateway

curl -s --cacert deploy/certs/ca.crt \
  --cert deploy/certs/engine/tls.crt --key deploy/certs/engine/tls.key \
  https://localhost:8443/v1/mcp/tools | jq .
```

Without TLS env vars the process still listens plaintext (tests / quick local only).

## Env

| Variable | Default |
|----------|---------|
| `SYMAGIOS_MCP_LISTEN` | `:8443` |
| `SYMAGIOS_MCP_REGISTRY` | `deploy/mcp/registry.json` |
| `SYMAGIOS_MCP_GATEWAY` | `https://mcp-gateway:8443` (engine client) |
| `SYMAGIOS_EGRESS_PROXY` | `https://envoy-egress:10001` |
| `SYMAGIOS_SOURCE` | `mcp-gateway` |
| `SYMAGIOS_MCP_TLS_CERT` / `_KEY` / `_CA` | unset → plaintext; set for mTLS server |
| `SYMAGIOS_MCP_ADMIN_TOKEN` | unset (loopback attach only) |
| `VAULT_*` | same as other Symagios services |
| `SYMAGIOS_CONTENT_GUARD` | `true` |
