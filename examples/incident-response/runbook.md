# Runbook — laptop exercise

1. Start auth profile (Envoy, OPA, Vault, Keycloak) if you want the full mesh:
   `make auth-install && ./scripts/compose.sh --profile auth up -d`
2. Start MCP gateway with the demo registry:
   `make mcp-gateway`
3. Optionally merge `mcp/registry.snippet.json` via attach (loopback or admin token).
4. Configure LLM (prefer `deploy/llm/config.yaml` under Compose):
   - Claude (Anthropic): `provider: claude` + `ANTHROPIC_API_KEY` / Vault `api_key`
   - Claude on Vertex: `provider: claude-vertex` + `VERTEX_PROJECT` / `VERTEX_LOCATION` + `VERTEX_ACCESS_TOKEN` (or ADC)
   - OpenCodex: `provider: opencodex` + `OPENCODEX_API_KEY` / `OPENAI_API_KEY`
5. Run engine: `cd pkg && go run ./cmd/symagios engine`
6. Feed `case/alert.json` through agent-entry with orchestrator + unit `data_source` bodies (see `HOW_TO_EXECUTE.md` §3).
7. Watch HITL: `go run ./cmd/symagios watch` — approve when a tool is missing or severity is high.

Expected teaching moments: missing MCP → human state; `send_email` with SSN → blocked; forged system prompt in user text → scrubbed.
