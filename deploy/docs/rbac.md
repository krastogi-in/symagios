# Default RBAC

Symagios ingress OPA checks **realm roles** on `x-symagios-roles` (fail-closed). Envoy maps Keycloak `realm_access.roles` into that header after JWT validation.

## Realm roles

Defined in `deploy/keycloak/symagios-realm.json`:

| Role | Intent |
|------|--------|
| `admin` | MCP attach/detach, full operate |
| `operator` | TUS agent write, `message/send`, `/peers` |
| `agent-runner` | MCP `tools/call`, `message/send` |

Path → role matrix: [mesh-hardening.md](mesh-hardening.md).

## Default principals

`./scripts/generate-dev-secrets.sh` creates passwords in `.env`. Vault seed + Keycloak sync bootstrap these users (no passwords in git):

| Env prefix | Default username | Roles |
|------------|------------------|-------|
| `SYMAGIOS_ADMIN_*` | `symagios-admin` | `admin` |
| `SYMAGIOS_OPERATOR_*` | `operator` | `operator`, `agent-runner` |
| `SYMAGIOS_RUNNER_*` | `runner` | `agent-runner` |

Keycloak’s built-in **admin** console user (`KEYCLOAK_ADMIN`) is separate — it manages the IdP, not Symagios OPA roles.

## Token smoke test

```bash
source .env
./scripts/keycloak-admin.sh token "$SYMAGIOS_OPERATOR_USER" "$SYMAGIOS_OPERATOR_PASSWORD" | jq -r .access_token
# Decode JWT: realm_access.roles should include operator / agent-runner
```
