# Symagios

Secured agent orchestration: agents, skills, and sub-agents run behind Envoy + OPA, with Vault-backed secrets, process jails, content guards, governed search, and an MCP gateway that only runs **attached** tools.

This repository is a **POC**. Expect to edit policies, registries, and example prompts as you harden.

## Architecture (short)

```text
Client (CLI / A2A)
    │ HTTPS :8443
    ▼
Envoy ingress ──JWT──► OPA (roles) ──► agent-gateway | agent-entry (TUS)
                                              │
                                              ▼
                                         Engine workers
                              (Landlock/seccomp + eBPF + contentguard)
                              LLM (Claude | OpenCodex) ← Vault api_key
                                 └ no provider search; model emits WEB_SEARCH JSON
                              MCP ──► mcp-gateway ──► stub|http|sse|stdio
                              WEB_SEARCH ──► search-broker ──► Tavily (egress)
                                              │
                                    missing MCP / HITL / search down
                                              ▼
                                         CLI watch
```

Egress (LLM, search, MCP upstreams) goes through Envoy egress + host allowlist + OPA.

## Quick start

```bash
make all          # build all binaries into bin/ + unit tests
make build        # binaries only
make tidy         # refresh go.mod/go.sum when needed
make docker-build # image (Podman by default; see scripts/lib/runtime.sh)

# Secured compose stack (auth mesh + engine / entry / gateway / search)
./scripts/stack-start.sh --build --watch   # start (auth-install if Vault down)
./scripts/stack-stop.sh                    # stop containers, keep DB
./scripts/stack-delete.sh                  # wipe SQLite volume (symagios-data)
# see HOW_TO_EXECUTE.md for uploads / Vertex / HITL

./bin/symagios engine
./bin/symagios agent-entry
./bin/symagios agent-gateway
./bin/mcp-gateway
```

Full mesh + Claude / incident-response exercise: see [HOW_TO_EXECUTE.md](HOW_TO_EXECUTE.md).

WIP note: follow-up actions use `TYPE::name{json}` parsing in `pkg/engine` (`ParseNextAction`). There is no separate agents runtime package — agent/skill/subagent are `ActionType` values on action runs.

Secrets prefer Vault (`VAULT_ADDR`, mTLS certs, path `symagios/llm` field `api_key`). Env keys are fine for local review.

## Major packages

| Path | Purpose |
|------|---------|
| `pkg/engine` | Workers, HITL, MCP client, LLM completion |
| `pkg/cmd/symagios` | One binary: `engine`, `watch`, `agent-entry`, `agent-gateway` |
| `pkg/llm` | Claude, Claude on Vertex, OpenCodex + guarded prompts |
| `pkg/mcp-gateway` | Attach catalog, Vault creds, http/sse/stdio/stub transports |
| `pkg/contentguard` | PII, jailbreak, prompt extract, exfil, tool egress re-check |
| `pkg/agent-entry` | TUS upload → `action_run` |
| `pkg/agent-gateway` | A2A |
| `pkg/search-gateway` | Governed web search |
| `deploy/opa` | Ingress/egress policy depth (roles, MCP, LLM hosts) |
| `deploy/envoy` | Edge TLS, ext_authz |
| `examples/incident-response` | Multi-agent use case for review |

## Docs

Start at [deploy/docs/README.md](deploy/docs/README.md):

- [auth-stack.md](deploy/docs/auth-stack.md) — how the mesh fits
- [mesh-hardening.md](deploy/docs/mesh-hardening.md) — OPA exposure, roles, TLS notes
- [content-guards.md](deploy/docs/content-guards.md) — LLM/memory/tool guards
- [mcp-gateway.md](deploy/docs/mcp-gateway.md) — attach, transports, HITL
- [guards.md](deploy/docs/guards.md) — Landlock/seccomp
- [governed-search.md](deploy/docs/governed-search.md)

## Example use case

See [examples/incident-response](examples/incident-response): orchestrator agent → triage skill → investigator sub-agent → reporter agent, with MCP and human escalation.

## What is still POC

- Agent packaging / upload of the markdown fixtures is not fully automated.
- OPA↔Envoy gRPC TLS is optional (documented); compose keeps that hop on the internal network only.
- MCP stdio/http/sse are real transports; production MCP servers must be allowlisted and Vault-backed.
- Content guards are heuristic; not a compliance product.
- Tenants / dynamic policy-manager are intentionally out of scope for now.

## License

See repository license file if present; otherwise treat as internal/private until published.
