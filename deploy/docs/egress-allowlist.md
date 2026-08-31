# What egress is allowed to dial

Envoy egress is easy to turn into an open proxy if you are not careful. We put a blunt host allowlist in Envoy RBAC **before** OPA. If the `:authority` (Host / CONNECT target) is not on the list, the request dies with 403 and never reaches policy.

That list is global for now — every caller shares it. Caller identity on this hop is the workload mTLS cert (Lua maps SAN/CN → `x-symagios-source`); OPA then scopes routes by that derived source. Longer term we should generate the allowlist from whatever network permissions skills or agents declare, so the Envoy regex and the product config cannot drift by hand.

Today the list is basically:

- `api.tavily.com` for governed search
- `api.anthropic.com` / `api.openai.com` for engine LLM calls
- Vertex Claude hosts: `*-aiplatform.googleapis.com`, `aiplatform.googleapis.com`, `aiplatform.{us,eu}.rep.googleapis.com`
- `localhost` / `127.0.0.1` for MCP upstream stubs (extend when attaching real hosts)
- `*.symagios.local` mesh names
- a few compose DNS names (`agent-gateway`, `envoy-ingress`, `envoy-egress`)

The regex that Envoy actually enforces lives in `deploy/envoy/envoy-egress.yaml`. If you add an external A2A peer or MCP server host, you must add its hostname there (and keep this mental list honest) **before** the caller tries to dial it. Search should stay on Tavily unless you deliberately expand the choke point. Mesh callers dial `SYMAGIOS_EGRESS_PROXY` with their workload client cert; source is cert-derived, not a client header.

One sharp edge: RBAC denials do not show up in OPA’s decision log. If you need one audit trail, lean on Envoy access logs (we already print egress lines to stdout) or unify that later.
