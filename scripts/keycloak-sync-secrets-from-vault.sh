#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
# shellcheck disable=SC1091
source ./scripts/lib/load-env.sh

VAULT_ADDR="${VAULT_ADDR:-http://localhost:8200}"
: "${VAULT_TOKEN:?VAULT_TOKEN missing — run ./scripts/generate-dev-secrets.sh}"
KEYCLOAK_REALM="${KEYCLOAK_REALM:-symagios}"
GATEWAY_CLIENT_ID="${GATEWAY_CLIENT_ID:-symagios-gateway}"
VAULT_GATEWAY_PATH="${VAULT_GATEWAY_PATH:-symagios/keycloak/clients/symagios-gateway}"

log() {
	echo "[keycloak-sync] $*"
}

die() {
	log "error: $*"
	exit 1
}

vault_field() {
	local path="$1" field="$2"
	./scripts/vault-admin.sh kv get "${path}" | jq -r ".data.data.${field}"
}

sync_gateway_client_secret() {
	local client_secret internal_id
	client_secret="$(vault_field "${VAULT_GATEWAY_PATH}" client_secret)"
	[[ -n "${client_secret}" && "${client_secret}" != "null" ]] || \
		die "missing client_secret at secret/${VAULT_GATEWAY_PATH}"

	internal_id="$(./scripts/keycloak-admin.sh kcadm get clients -r "${KEYCLOAK_REALM}" \
		-q "clientId=${GATEWAY_CLIENT_ID}" --fields id | jq -r '.[0].id // empty')"
	[[ -n "${internal_id}" ]] || die "keycloak client not found: ${GATEWAY_CLIENT_ID}"

	log "updating Keycloak client secret for ${GATEWAY_CLIENT_ID} from Vault"
	./scripts/keycloak-admin.sh kcadm update "clients/${internal_id}" -r "${KEYCLOAK_REALM}" \
		-s "secret=${client_secret}"

	log "synced ${GATEWAY_CLIENT_ID} client secret from Vault"
}

# ensure_rbac_user <vault-slug> <env-user> <env-password> <env-roles-default>
ensure_rbac_user() {
	local slug="$1" env_user="$2" env_pass="$3" default_roles="$4"
	local username password roles role
	local path="symagios/keycloak/users/${slug}"

	username="$(vault_field "${path}" username 2>/dev/null || true)"
	password="$(vault_field "${path}" password 2>/dev/null || true)"
	roles="$(vault_field "${path}" roles 2>/dev/null || true)"
	if [[ -z "${username}" || "${username}" == "null" || -z "${password}" || "${password}" == "null" ]]; then
		username="${env_user}"
		password="${env_pass}"
		roles="${default_roles}"
	fi
	if [[ -z "${username}" || -z "${password}" ]]; then
		log "no ${slug} user in Vault/.env — skip"
		return 0
	fi
	roles="${roles:-${default_roles}}"

	if ./scripts/keycloak-admin.sh user get "${username}" >/dev/null 2>&1; then
		log "RBAC user ${username} exists — refreshing password/roles"
		./scripts/keycloak-admin.sh user set-password "${username}" "${password}"
	else
		log "creating RBAC user ${username} (roles=${roles})"
		./scripts/keycloak-admin.sh user create "${username}" \
			--email "${username}@localhost" \
			--first "${username}" --last User \
			--password "${password}"
	fi

	IFS=',' read -ra role_list <<<"${roles}"
	for role in "${role_list[@]}"; do
		role="$(echo "${role}" | tr -d '[:space:]')"
		[[ -n "${role}" ]] || continue
		./scripts/keycloak-admin.sh kcadm add-roles -r "${KEYCLOAK_REALM}" \
			--uusername "${username}" --rolename "${role}" >/dev/null 2>&1 || true
	done

	if ./scripts/keycloak-admin.sh token "${username}" "${password}" >/dev/null 2>&1; then
		log "token check ok for ${username}"
	else
		log "warning: token check failed for ${username}"
	fi
}

bootstrap_rbac_users() {
	ensure_rbac_user admin \
		"${SYMAGIOS_ADMIN_USER:-}" \
		"${SYMAGIOS_ADMIN_PASSWORD:-}" \
		"${SYMAGIOS_ADMIN_ROLES:-admin}"
	ensure_rbac_user operator \
		"${SYMAGIOS_OPERATOR_USER:-}" \
		"${SYMAGIOS_OPERATOR_PASSWORD:-}" \
		"${SYMAGIOS_OPERATOR_ROLES:-operator,agent-runner}"
	ensure_rbac_user runner \
		"${SYMAGIOS_RUNNER_USER:-}" \
		"${SYMAGIOS_RUNNER_PASSWORD:-}" \
		"${SYMAGIOS_RUNNER_ROLES:-agent-runner}"
}

command -v jq >/dev/null 2>&1 || die "jq is required"

log "reading secrets from Vault"
sync_gateway_client_secret
bootstrap_rbac_users

cat <<EOF

Keycloak client secret synced; default RBAC users bootstrapped.

Vault path : secret/${VAULT_GATEWAY_PATH}
Users      : secret/symagios/keycloak/users/{admin,operator,runner}
Keycloak   : realm=${KEYCLOAK_REALM} client=${GATEWAY_CLIENT_ID}

See deploy/docs/rbac.md

EOF
