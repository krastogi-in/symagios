#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
# shellcheck disable=SC1091
source ./scripts/lib/runtime.sh

# Load generated secrets (never commit .env).
if [[ ! -f .env ]]; then
	echo "[install-vault] missing .env — generating local secrets" >&2
	./scripts/generate-dev-secrets.sh
fi
set -a
# shellcheck disable=SC1091
source .env
set +a

: "${VAULT_TOKEN:?VAULT_TOKEN missing in .env}"
: "${KEYCLOAK_ADMIN_PASSWORD:?KEYCLOAK_ADMIN_PASSWORD missing in .env}"
: "${SYMAGIOS_GATEWAY_CLIENT_SECRET:?SYMAGIOS_GATEWAY_CLIENT_SECRET missing in .env}"
: "${SYMAGIOS_ENGINE_ENCRYPTION_KEY:?SYMAGIOS_ENGINE_ENCRYPTION_KEY missing in .env}"

VAULT_ADDR="${VAULT_ADDR:-http://localhost:8200}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-60}"
KEYCLOAK_ADMIN="${KEYCLOAK_ADMIN:-admin}"

log() {
	echo "[install-vault] $*"
}

require_command() {
	command -v "$1" >/dev/null 2>&1 || { echo "missing required command: $1" >&2; exit 1; }
}

vault_cli() {
	symagios_auth_compose exec -T \
		-e VAULT_ADDR=http://127.0.0.1:8200 \
		-e VAULT_TOKEN="${VAULT_TOKEN}" \
		vault vault "$@"
}

wait_for_vault() {
	local deadline=$((SECONDS + WAIT_TIMEOUT))
	while (( SECONDS < deadline )); do
		if symagios_auth_compose exec -T vault \
			wget -qO- http://127.0.0.1:8200/v1/sys/health >/dev/null 2>&1; then
			return 0
		fi
		sleep 2
	done
	echo "vault did not become ready within ${WAIT_TIMEOUT}s" >&2
	return 1
}

seed_keycloak_secrets() {
	log "seeding keycloak / engine secrets into Vault KV (from .env)"
	vault_cli kv put secret/symagios/keycloak/admin \
		username="${KEYCLOAK_ADMIN}" password="${KEYCLOAK_ADMIN_PASSWORD}"
	vault_cli kv put secret/symagios/keycloak/clients/symagios-gateway \
		client_id=symagios-gateway client_secret="${SYMAGIOS_GATEWAY_CLIENT_SECRET}"
	vault_cli kv put secret/symagios/keycloak/clients/symagios-cli \
		client_id=symagios-cli public=true
	vault_cli kv put secret/symagios/engine \
		encryption_key="${SYMAGIOS_ENGINE_ENCRYPTION_KEY}"
	vault_cli kv put secret/symagios/tavily \
		api_key="${TAVILY_API_KEY:-tvly-dev-placeholder}"
	# LLM key optional at seed time; engine reads when present.
	vault_cli kv put secret/symagios/llm \
		api_key="${LLM_API_KEY:-${ANTHROPIC_API_KEY:-set-me}}"

	seed_rbac_user() {
		local slug="$1" user="$2" pass="$3" roles="$4"
		[[ -n "${user}" && -n "${pass}" ]] || return 0
		vault_cli kv put "secret/symagios/keycloak/users/${slug}" \
			username="${user}" password="${pass}" roles="${roles}"
	}

	# Default RBAC principals (passwords from .env; roles map to OPA has_role).
	seed_rbac_user admin \
		"${SYMAGIOS_ADMIN_USER:-}" \
		"${SYMAGIOS_ADMIN_PASSWORD:-}" \
		"${SYMAGIOS_ADMIN_ROLES:-admin}"
	seed_rbac_user operator \
		"${SYMAGIOS_OPERATOR_USER:-}" \
		"${SYMAGIOS_OPERATOR_PASSWORD:-}" \
		"${SYMAGIOS_OPERATOR_ROLES:-operator,agent-runner}"
	seed_rbac_user runner \
		"${SYMAGIOS_RUNNER_USER:-}" \
		"${SYMAGIOS_RUNNER_PASSWORD:-}" \
		"${SYMAGIOS_RUNNER_ROLES:-agent-runner}"
}

setup_pki_example() {
	log "configuring example PKI mount (for future workload identity)"
	if vault_cli secrets list -format=json | jq -e '.["pki/"]' >/dev/null 2>&1; then
		log "pki mount already enabled"
		return 0
	fi

	vault_cli secrets enable pki
	vault_cli secrets tune -max-lease-ttl=87600h pki
	vault_cli write -format=json pki/root/generate/internal \
		common_name=symagios.local ttl=87600h >/dev/null
	vault_cli write pki/config/urls \
		issuing_certificates="http://vault:8200/v1/pki/ca" \
		crl_distribution_points="http://vault:8200/v1/pki/crl"
	vault_cli write pki/roles/symagios-workload \
		allowed_domains=symagios.local \
		allow_subdomains=true \
		allow_bare_domains=true \
		allow_any_name=true \
		allow_localhost=true \
		allow_ip_sans=true \
		max_ttl=72h \
		key_type=rsa \
		key_bits=2048 \
		client_flag=true \
		server_flag=true
}

apply_policies() {
	log "applying Vault policies"
	local policy
	for policy in \
		symagios-read \
		symagios-admin \
		symagios-engine \
		symagios-agent-entry \
		symagios-agent-gateway \
		symagios-search-broker \
		symagios-mcp-gateway \
		symagios-keycloak; do
		vault_cli policy write "${policy}" "/vault/policies/${policy}.hcl"
	done
}

issue_workload_token() {
	local policy="$1"
	vault_cli token create \
		-policy="${policy}" \
		-display-name="${policy}" \
		-ttl=720h \
		-renewable=true \
		-format=json | jq -r '.auth.client_token'
}

# Per-workload tokens (not root). Written to .env.vault-workloads and merged into .env
# so compose ${VAULT_TOKEN_*} interpolation works.
issue_workload_tokens() {
	log "issuing per-workload Vault tokens (scoped policies)"
	local out=".env.vault-workloads"
	local engine_tok entry_tok gateway_tok search_tok mcp_tok keycloak_tok
	engine_tok="$(issue_workload_token symagios-engine)"
	entry_tok="$(issue_workload_token symagios-agent-entry)"
	gateway_tok="$(issue_workload_token symagios-agent-gateway)"
	search_tok="$(issue_workload_token symagios-search-broker)"
	mcp_tok="$(issue_workload_token symagios-mcp-gateway)"
	keycloak_tok="$(issue_workload_token symagios-keycloak)"

	cat >"${out}" <<EOF
# Generated by scripts/install-vault.sh — do not commit.
# Workload tokens are scoped by policy; VAULT_TOKEN in .env remains the root/admin token.

VAULT_TOKEN_ENGINE=${engine_tok}
VAULT_TOKEN_AGENT_ENTRY=${entry_tok}
VAULT_TOKEN_AGENT_GATEWAY=${gateway_tok}
VAULT_TOKEN_SEARCH_BROKER=${search_tok}
VAULT_TOKEN_MCP_GATEWAY=${mcp_tok}
VAULT_TOKEN_KEYCLOAK=${keycloak_tok}
EOF
	chmod 600 "${out}"

	if [[ -f .env ]]; then
		local tmp
		tmp="$(mktemp)"
		grep -vE '^VAULT_TOKEN_(ENGINE|AGENT_ENTRY|AGENT_GATEWAY|SEARCH_BROKER|MCP_GATEWAY|KEYCLOAK)=' .env >"${tmp}" || true
		grep -E '^VAULT_TOKEN_' "${out}" >>"${tmp}"
		mv "${tmp}" .env
		chmod 600 .env
		log "merged workload tokens into .env"
	fi
}

require_command curl
require_command jq
symagios_require_compose

# Compose interpolates ALL services' ${VAULT_TOKEN_*:?} even when only starting
# vault — so empty/missing workload tokens fail before we can issue them.
# Seed non-empty placeholders; issue_workload_tokens() replaces them after Vault is up.
ensure_workload_token_placeholders() {
	local key
	for key in \
		VAULT_TOKEN_ENGINE \
		VAULT_TOKEN_AGENT_ENTRY \
		VAULT_TOKEN_AGENT_GATEWAY \
		VAULT_TOKEN_SEARCH_BROKER \
		VAULT_TOKEN_MCP_GATEWAY \
		VAULT_TOKEN_KEYCLOAK; do
		if [[ -z "${!key:-}" ]]; then
			export "${key}=pending"
			if [[ -f .env ]]; then
				if grep -qE "^${key}=" .env; then
					# Replace empty assignment so future compose loads work.
					local tmp
					tmp="$(mktemp)"
					grep -vE "^${key}=" .env >"${tmp}" || true
					echo "${key}=pending" >>"${tmp}"
					mv "${tmp}" .env
					chmod 600 .env
				else
					echo "${key}=pending" >>.env
				fi
			fi
		fi
	done
}

ensure_workload_token_placeholders

log "starting vault (dev mode; root token from .env)"
symagios_auth_compose up -d vault

log "waiting for vault"
wait_for_vault

seed_keycloak_secrets
setup_pki_example
apply_policies
issue_workload_tokens

cat <<EOF

Symagios Vault is running (dev mode).

API (inside the vault container only): http://127.0.0.1:8200
Other containers: https://vault.symagios.local:8202 (mTLS, client cert required)
Root token   : (see .env VAULT_TOKEN — admin/scripts only, not printed)
Workload tokens: .env.vault-workloads (merged into .env as VAULT_TOKEN_*)

Keycloak secrets (KV v2):
  secret/symagios/keycloak/admin
  secret/symagios/keycloak/clients/symagios-gateway
  secret/symagios/keycloak/clients/symagios-cli
  secret/symagios/keycloak/users/{admin,operator,runner}
  secret/symagios/engine
  secret/symagios/llm
  secret/symagios/tavily

Example read:
  ./scripts/vault-admin.sh kv get symagios/keycloak/clients/symagios-gateway

Issue container mTLS certs (Vault PKI):
  make mtls-issue
  make mtls-up

Sync secrets into Keycloak:
  ./scripts/keycloak-sync-secrets-from-vault.sh

Mapping reference:
  deploy/docs/vault-keycloak.md

Stop Vault:
  ./scripts/compose.sh --profile auth stop vault

EOF
