# How the auth stack fits together

Symagios is a secured agent orchestrator. On a laptop we run that idea as a Docker Compose “auth” profile: Keycloak for login, Envoy as the only door to the outside world, OPA for allow/deny, Vault for secrets and certificates, then our own services behind that.

Nothing in this folder is loaded by the programs themselves. Compose, `deploy/envoy/`, `deploy/opa/`, and the scripts under `scripts/` are what actually run. This page is just the story of how those pieces talk.

## What an outside caller sees

From your machine there is basically one public HTTPS URL: `https://localhost:8443`. That is Envoy ingress. You do not publish Keycloak, OPA, agent-gateway, or agent-entry on the host. If something needs to come in from outside, it goes through Envoy first.

Envoy’s admin port sits on loopback inside the container and is not mapped to the host. Health checks use `./scripts/compose.sh exec` and poke that loopback address.

## Logging in (Keycloak)

We use Keycloak for OIDC. The realm is `symagios`. Tokens are issued so that the issuer looks like `https://localhost:8443/realms/symagios` — that is, the Envoy URL people actually use — not some internal Keycloak hostname. Envoy adds `x-forwarded-proto: https` on the realm routes so Keycloak issues tokens with that issuer.

There are two OIDC clients:

- `symagios-cli` — public client for local password grant (`dev` / `dev` in the sample realm).
- `symagios-gateway` — confidential client. Its secret is **not** checked into the realm JSON. It lives in Vault and is applied when Keycloak starts. See [vault-keycloak.md](vault-keycloak.md).

When the CLI wants a token it calls Envoy’s token URL over HTTPS (with our CA). Envoy then talks to Keycloak over mTLS on Keycloak’s HTTPS port. Keycloak still has an HTTP port on `127.0.0.1:8080` inside its own container so `kcadm` can administer it without presenting a client certificate. That HTTP port is not published to the host.

Envoy caches JWKS from Keycloak (about five minutes) and verifies JWTs itself. In-flight requests keep using the keys Envoy already has; a refresh happens in the background when the cache ages out or an unknown `kid` shows up.

## Getting through the door (Envoy + OPA)

On the way in, Envoy roughly does:

1. Terminate TLS (see [edge-tls.md](edge-tls.md)).
2. Tag the request as ingress traffic and strip client `x-user-id` (Lua).
3. Check the JWT (unless the path is public, like `/.well-known/` or the OIDC token/auth endpoints). On success, `claim_to_headers` sets `x-user-id` from `preferred_username` and forwards `x-jwt-payload`.
4. Ask OPA over gRPC whether the call is allowed (`data.envoy.authz.allow`). If OPA is down, we fail closed.
5. Forward to the right upstream over east-west mTLS (Envoy presents `envoy-ingress`).

Today that means:

- A2A paths (`/message/send`, tasks, agent card) go to **agent-gateway**.
- Agent upload / start (`/agents…` TUS) goes to **agent-entry** via the `config_server` cluster.

**agent-entry** (human/TUS path) requires the peer to be `envoy-ingress` plus JWT-derived `X-User-ID` and `X-Jwt-Payload` (set only after Envoy jwt_authn against Keycloak JWKS). Apps do not re-verify JWTs — identity stays at Envoy + Keycloak. **agent-gateway** also accepts mesh peers: `agent-gateway` / `envoy-egress` client certs with a synthetic `X-User-ID` and no JWT (A2A peer hops). The public agent card (`/.well-known/`) skips app auth on purpose.

OPA policies live under `deploy/opa/policies/envoy/`. Ingress and egress share one decision entrypoint; a header (`x-symagios-traffic`) picks which package applies. Ingress role checks are **fail-closed**: JWT realm roles must appear in `x-symagios-roles` (Envoy Lua maps `realm_access.roles`). The roles Lua **always clears** any client-supplied `x-symagios-roles` first; it only adds roles when the JWT payload parses. Missing/empty roles deny privileged paths even when the caller is authenticated.

## Leaving the mesh (Envoy egress)

Outbound calls (external agents, Tavily search, LLM providers, MCP upstreams) go through a second Envoy: egress, mesh-only at `https://envoy-egress:10001` (not published to the host). Callers must present a Vault-PKI workload client cert; Envoy Lua strips any client `X-Symagios-Source` and sets it from the peer cert DNS SAN / CN (`symagios-engine`, `mcp-gateway`, `agent-gateway`). Spoofed source headers are ignored.

Callers set `SYMAGIOS_EGRESS_PROXY` and dial with mTLS (`VAULT_CACERT` + `VAULT_CLIENT_CERT` / `VAULT_CLIENT_KEY`). `pkg/egress` implements the Host rewrite for LLM, MCP, search-broker, and agent-gateway.

Egress is intentionally thinner than ingress. It does not re-do Keycloak JWT the same way. It:

1. Requires client certs and derives source identity from the cert.
2. Tags traffic as egress.
3. Runs a hard host allowlist in Envoy RBAC (cheap deny before OPA).
4. Asks OPA about source + path (+ host for search / A2A CONNECT).
5. Forwards with dynamic forward proxy, including HTTPS CONNECT when needed.

See [egress-allowlist.md](egress-allowlist.md) and [governed-search.md](governed-search.md).

## What sits behind Envoy

**agent-gateway** speaks A2A: agent card, message/send, task get/list/cancel, and peer add/remove (`POST`/`DELETE` `/peers`).

**agent-entry**: Start point for the agent. A client opens a TUS upload with `POST /agents`, which creates an empty workflow manifest. Chunks land with `PATCH /agents/{session_id}/{agent_name}` (same shape for HEAD/DELETE). When the upload is complete we encrypt the payload with the engine key from Vault and flip the run to `PENDING` so the engine can pick it up. There is no agent directory listing; admins inspect SQLite directly.

**engine** runs the action_runs. Its AES key comes from Vault (`secret/symagios/engine`), not from a CLI argument.

## Vault

Vault holds the secrets we refuse to duplicate in git, and it issues the workload certificates we use for mTLS. Details in [vault-keycloak.md](vault-keycloak.md) and [mtls.md](mtls.md).

## How to bring it up

`make auth-install` now does this in order:

1. Generate `.env` secrets  
2. Vault (dev) + KV seed + PKI role  
3. **Issue mTLS certs** into `deploy/certs/`  
4. Start **vault-mtls** (Keycloak needs `:8202`)
5. Keycloak → OPA → Envoy  

You can still run the steps by hand:

```bash
make secrets-generate
make vault-install
make mtls-issue
./scripts/compose.sh --profile auth up -d vault-mtls
make keycloak-install
make opa-install
make envoy-install
```
