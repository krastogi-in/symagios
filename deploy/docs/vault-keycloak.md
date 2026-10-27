# Vault and Keycloak

Vault is the only place we intentionally store that secret. Keycloak learns it at startup.

## Local secrets (B5)

Compose and install scripts **do not** embed `root` / `admin` / `dev` passwords anymore.

1. `./scripts/generate-dev-secrets.sh` (or `make secrets-generate`) writes a gitignored `.env` with random Vault **root** token, Keycloak admin password, gateway client secret, MCP admin token, engine AES key, and RBAC user passwords.
2. `make vault-install` seeds KV, applies **per-workload policies**, and issues scoped tokens into `.env.vault-workloads` (also merged into `.env` as `VAULT_TOKEN_ENGINE`, `VAULT_TOKEN_AGENT_ENTRY`, …). Compose workloads use those — not the root token.
3. `./scripts/compose.sh` (Podman by default) substitutes `${VAULT_TOKEN_*:?…}` from `.env` — missing vars fail loudly.
4. Keycloak admin password still arrives via `keycloak-vault-fetch` (token: `VAULT_TOKEN_KEYCLOAK`); the realm JSON has **no** users with passwords. Default **RBAC** users are created by `keycloak-sync` — see [rbac.md](rbac.md).

Never commit `.env` or `.env.vault-workloads`. `.env.example` shows the shape only.

## What Vault is doing here

In compose, Vault still runs in **dev mode**, but the root token ID comes from `.env` (`VAULT_DEV_ROOT_TOKEN_ID` / `VAULT_TOKEN`). That token is for **install/admin scripts only**. Workloads get renewable tokens scoped to the paths they need (`deploy/vault/policies/symagios-*.hcl`). The plaintext API listens only on `127.0.0.1:8200` inside the vault container. Other services do not call that address over the Docker network.

Instead they call `https://vault.symagios.local:8202`, which is an Envoy sidecar sharing Vault’s network namespace and requiring a client certificate. See [mtls.md](mtls.md) for why that sidecar exists.

`make vault-install` seeds a few KV paths under `secret/symagios/…`, including:

- Keycloak admin password
- `symagios-gateway` client secret
- Operator user credentials + roles
- Engine encryption key (32-byte AES material)
- LLM API key placeholder (`secret/symagios/llm`, override with `LLM_API_KEY` / `ANTHROPIC_API_KEY` when seeding)
- Tavily API key placeholder (override with `TAVILY_API_KEY` when seeding if you have a real one)

It also turns on the PKI mount used for workload certs and issues **scoped workload tokens** (engine, agent-entry, agent-gateway, search-broker, mcp-gateway, keycloak-fetch).

## How Keycloak gets the gateway secret

1. Vault must be healthy and seeded.
2. Install/`stack-start` runs `keycloak-vault-fetch` as **`compose run --rm`** (`auth-fetch` profile) — not `up`. That presents the Keycloak workload cert to Vault over mTLS, reads the secret, and writes it into a shared volume, then removes the container (no `Exited` one-shot left in `ps`).
3. The Keycloak container waits for those files, starts `start-dev` with realm import, then uses `kcadm` on local HTTP to set the `symagios-gateway` client secret to whatever Vault had (writes `/tmp/symagios-kc-bootstrap.done` when done).
4. Install/sync bootstraps the operator user and assigns realm roles used by OPA (`x-symagios-roles`).

The realm JSON still describes the client as confidential, but it does **not** embed the secret value. After sync, Keycloak’s own DB holds the live secret — that is runtime state, not a second git copy.

If you rotate the secret in Vault later, you can re-run `scripts/keycloak-sync-secrets-from-vault.sh` (or restart the fetch + Keycloak path). Day-to-day install no longer depends on that script because the entrypoint does the first sync; install-keycloak re-runs sync for the operator user.

## Engine encryption key

`symagios engine` and `agent-entry` no longer take the AES key as a CLI argument. They read `encryption_key` from `secret/symagios/engine` (with optional `SYMAGIOS_ENCRYPTION_KEY` only for tests). In compose they mount the engine certs so they can talk to Vault over mTLS.

## Mental model

Vault = write secrets and issue certs once.  
Keycloak = identity and tokens.  
Envoy = the only public door, and the mTLS front for Vault-dev.  
Nobody should be pasting the gateway client secret into a file that lives in git again.
