# Deploy docs

These notes explain how the local Symagios auth / TLS / search setup fits together.

Start with [auth-stack.md](auth-stack.md) if you want the whole picture. The other files go deeper on one topic:

- [edge-tls.md](edge-tls.md) — why the only public port is HTTPS
- [mtls.md](mtls.md) — how containers prove who they are with certificates
- [vault-keycloak.md](vault-keycloak.md) — where secrets live and how Keycloak gets them
- [egress-allowlist.md](egress-allowlist.md) — what outbound hosts we allow
- [governed-search.md](governed-search.md) — why search goes through us, not the LLM provider
- [rbac.md](rbac.md) — default realm roles and bootstrap users
- [guards.md](guards.md) — Landlock + seccomp for agents, skills, scripts
- [content-guards.md](content-guards.md) — PII/secrets guards on LLM in/out
- [mcp-gateway.md](mcp-gateway.md) — secured MCP attach, Vault creds, missing tool → HITL
- [mesh-hardening.md](mesh-hardening.md) — OPA exposure, roles, optional TLS
