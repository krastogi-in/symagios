#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
# shellcheck disable=SC1091
source ./scripts/lib/load-env.sh
# shellcheck disable=SC1091
source ./scripts/lib/runtime.sh

KEYCLOAK_URL="${KEYCLOAK_URL:-https://localhost:8443}"
KEYCLOAK_ADMIN="${KEYCLOAK_ADMIN:-admin}"
KEYCLOAK_REALM="${KEYCLOAK_REALM:-symagios}"
KEYCLOAK_CLI_CLIENT="${KEYCLOAK_CLI_CLIENT:-symagios-cli}"
# Must differ from the entrypoint's /tmp/symagios-entrypoint-kcadm.config or concurrent
# kcadm calls fight over the file lock ("Failed to get lock on …/kcadm.config").
KC_CONFIG="${KC_CONFIG:-/tmp/symagios-admin-kcadm.config}"

if [[ -z "${KEYCLOAK_ADMIN_PASSWORD:-}" ]]; then
	echo "KEYCLOAK_ADMIN_PASSWORD unset — run ./scripts/generate-dev-secrets.sh" >&2
	exit 1
fi

usage() {
	cat <<'EOF'
Symagios Keycloak admin CLI (uses kcadm.sh inside the Keycloak container).

Usage:
  ./scripts/keycloak-admin.sh <command> [args]

Commands:
  status                          Check Keycloak health
  login                           Store admin credentials for kcadm
  realm list                      List realms
  realm get                       Show symagios realm
  user list                       List users in symagios realm
  user get <username>             Show user details
  user create <username> [opts]   Create user
  user delete <username>          Delete user
  user set-password <user> <pw>   Set user password
  client list                     List clients in symagios realm
  client get <client-id>          Show client details
  client secret <client-id>       Show client secret (confidential clients)
  token <username> <password>     Get access token via symagios-cli
  kcadm <args...>                 Run raw kcadm command

User create options:
  --email <email>
  --first <first-name>
  --last <last-name>
  --password <password>           If omitted, user is created without credentials

Requires: podman (or docker), curl, jq (for user/client lookups)

Environment:
  KEYCLOAK_URL              Default: https://localhost:8443 (Envoy TLS; not Keycloak :8080)
  KEYCLOAK_ADMIN            From .env (default username admin)
  KEYCLOAK_ADMIN_PASSWORD   Required from .env (no default)
  KEYCLOAK_REALM            Default: symagios
  KEYCLOAK_CLI_CLIENT       Default: symagios-cli

Examples:
  ./scripts/keycloak-admin.sh user list
  ./scripts/keycloak-admin.sh user create alice --email alice@example.com --password '<from-.env>'
  ./scripts/keycloak-admin.sh token operator "$SYMAGIOS_OPERATOR_PASSWORD"
  ./scripts/keycloak-admin.sh kcadm get users -r symagios

EOF
}

log() {
	echo "[keycloak-admin] $*" >&2
}

die() {
	log "error: $*"
	exit 1
}

require_command() {
	command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

ensure_runtime() {
	symagios_require_compose
}

ensure_keycloak_running() {
	if ! symagios_auth_compose exec -T keycloak true >/dev/null 2>&1; then
		die "keycloak is not running; run: make keycloak-up"
	fi
}

kcadm() {
	ensure_keycloak_running
	# Put --config after the verb (Keycloak 26 rejects `kcadm.sh --config … <verb>`).
	symagios_auth_compose exec -T \
		-e "KCADM_CONFIG=${KC_CONFIG}" \
		keycloak \
		/opt/keycloak/bin/kcadm.sh "$@" --config "${KC_CONFIG}"
}

kcadm_login() {
	ensure_keycloak_running
	local err
	# Use 127.0.0.1 — Keycloak binds IPv4 only; `localhost` → ::1 often fails in-container.
	# Password via env (not argv) so compose failure lines do not print the secret.
	# Separate --config path avoids lock fights with the entrypoint's kcadm.
	err="$(
		symagios_auth_compose exec -T \
			-e "KCADM_USER=${KEYCLOAK_ADMIN}" \
			-e "KCADM_PASSWORD=${KEYCLOAK_ADMIN_PASSWORD}" \
			-e "KCADM_CONFIG=${KC_CONFIG}" \
			keycloak \
			sh -c '/opt/keycloak/bin/kcadm.sh config credentials \
				--server http://127.0.0.1:8080 \
				--realm master \
				--user "$KCADM_USER" \
				--password "$KCADM_PASSWORD" \
				--config "$KCADM_CONFIG"' 2>&1
	)" || {
		log "kcadm login failed (admin=${KEYCLOAK_ADMIN} @ http://127.0.0.1:8080/master)"
		[[ -n "${err}" ]] && printf '%s\n' "${err}" >&2
		log "if Invalid credentials: admin password in Keycloak DB drifted from .env — wipe and re-bootstrap:"
		log "  ./scripts/stack-delete.sh --all --yes && ./scripts/stack-start.sh --auth-force"
		die "kcadm config credentials failed"
	}
}

with_admin() {
	kcadm_login
	kcadm "$@"
}

cmd_status() {
	require_command curl
	if symagios_auth_compose exec -T keycloak \
		sh -c "exec 3<>/dev/tcp/127.0.0.1/8080; echo -e 'GET /realms/master HTTP/1.1\r\nhost: localhost\r\nConnection: close\r\n\r\n' >&3; grep -q '200 OK' <&3" \
		>/dev/null 2>&1; then
		echo "keycloak: ready inside container (HTTP :8080 is not published)"
		echo "tokens:  ${KEYCLOAK_URL}/realms/${KEYCLOAK_REALM}/protocol/openid-connect/token"
	else
		die "keycloak is not ready inside the container"
	fi
}

cmd_login() {
	kcadm_login
	echo "admin credentials stored in container at ${KC_CONFIG}"
}

cmd_realm_list() {
	with_admin get realms
}

cmd_realm_get() {
	with_admin get "realms/${KEYCLOAK_REALM}"
}

user_id_for() {
	local username="$1"
	with_admin get users -r "${KEYCLOAK_REALM}" -q "username=${username}" --fields id,username | \
		jq -r '.[0].id // empty'
}

cmd_user_list() {
	with_admin get users -r "${KEYCLOAK_REALM}"
}

cmd_user_get() {
	local username="${1:-}"
	[[ -n "${username}" ]] || die "usage: user get <username>"
	local raw
	raw="$(with_admin get users -r "${KEYCLOAK_REALM}" -q "username=${username}")"
	if [[ "$(echo "${raw}" | jq -r 'length')" == "0" ]]; then
		die "user not found: ${username}"
	fi
	echo "${raw}"
}

cmd_user_create() {
	local username="${1:-}"
	shift || true
	[[ -n "${username}" ]] || die "usage: user create <username> [--email ...] [--password ...]"

	local email="" first="" last="" password=""
	while [[ $# -gt 0 ]]; do
		case "$1" in
			--email) email="$2"; shift 2 ;;
			--first) first="$2"; shift 2 ;;
			--last) last="$2"; shift 2 ;;
			--password) password="$2"; shift 2 ;;
			*) die "unknown option: $1" ;;
		esac
	done

	local args=(
		create users -r "${KEYCLOAK_REALM}"
		-s "username=${username}"
		-s enabled=true
	)
	# Keycloak VERIFY_PROFILE blocks password-grant until first/last are set.
	first="${first:-${username}}"
	last="${last:-User}"
	[[ -n "${email}" ]] && args+=(-s "email=${email}" -s emailVerified=true)
	args+=(-s "firstName=${first}" -s "lastName=${last}" -s "requiredActions=[]")

	with_admin "${args[@]}"

	if [[ -n "${password}" ]]; then
		cmd_user_set_password "${username}" "${password}"
	fi
}

cmd_user_delete() {
	local username="${1:-}"
	[[ -n "${username}" ]] || die "usage: user delete <username>"
	local user_id
	user_id="$(user_id_for "${username}")"
	[[ -n "${user_id}" ]] || die "user not found: ${username}"
	with_admin delete "users/${user_id}" -r "${KEYCLOAK_REALM}"
}

cmd_user_set_password() {
	local username="${1:-}" password="${2:-}"
	[[ -n "${username}" && -n "${password}" ]] || die "usage: user set-password <username> <password>"
	local user_id
	user_id="$(user_id_for "${username}")"
	[[ -n "${user_id}" ]] || die "user not found: ${username}"
	# Permanent password: temporary=true yields "Account is not fully set up" on ROPC.
	with_admin set-password -r "${KEYCLOAK_REALM}" --userid "${user_id}" \
		--new-password "${password}" --temporary=false
	# VERIFY_PROFILE requires first/last; without them password-grant fails the same way.
	with_admin update "users/${user_id}" -r "${KEYCLOAK_REALM}" \
		-s "firstName=${username}" -s lastName=User \
		-s emailVerified=true -s "requiredActions=[]" >/dev/null
}

cmd_client_list() {
	with_admin get clients -r "${KEYCLOAK_REALM}"
}

cmd_client_get() {
	local client_id="${1:-}"
	[[ -n "${client_id}" ]] || die "usage: client get <client-id>"
	with_admin get clients -r "${KEYCLOAK_REALM}" -q "clientId=${client_id}"
}

cmd_client_secret() {
	local client_id="${1:-}"
	[[ -n "${client_id}" ]] || die "usage: client secret <client-id>"
	local internal_id
	internal_id="$(with_admin get clients -r "${KEYCLOAK_REALM}" -q "clientId=${client_id}" --fields id | jq -r '.[0].id // empty')"
	[[ -n "${internal_id}" ]] || die "client not found: ${client_id}"
	with_admin get "clients/${internal_id}/client-secret" -r "${KEYCLOAK_REALM}"
}

cmd_token() {
	local username="${1:-}" password="${2:-}"
	[[ -n "${username}" && -n "${password}" ]] || die "usage: token <username> <password>"
	require_command curl
	curl -fsS --cacert deploy/certs/ca.crt -X POST "${KEYCLOAK_URL}/realms/${KEYCLOAK_REALM}/protocol/openid-connect/token" \
		-d "client_id=${KEYCLOAK_CLI_CLIENT}" \
		-d "grant_type=password" \
		-d "username=${username}" \
		-d "password=${password}"
}

cmd_kcadm() {
	[[ $# -gt 0 ]] || die "usage: kcadm <args...>"
	with_admin "$@"
}

main() {
	local cmd="${1:-help}"
	shift || true

	ensure_runtime

	case "${cmd}" in
		help|-h|--help) usage ;;
		status) cmd_status ;;
		login) cmd_login ;;
		realm)
			case "${1:-}" in
				list) cmd_realm_list ;;
				get) cmd_realm_get ;;
				*) die "usage: realm list|get" ;;
			esac
			;;
		user)
			case "${1:-}" in
				list) cmd_user_list ;;
				get) shift; cmd_user_get "$@" ;;
				create) shift; cmd_user_create "$@" ;;
				delete) shift; cmd_user_delete "$@" ;;
				set-password) shift; cmd_user_set_password "$@" ;;
				*) die "usage: user list|get|create|delete|set-password" ;;
			esac
			;;
		client)
			case "${1:-}" in
				list) cmd_client_list ;;
				get) shift; cmd_client_get "$@" ;;
				secret) shift; cmd_client_secret "$@" ;;
				*) die "usage: client list|get|secret" ;;
			esac
			;;
		token) cmd_token "$@" ;;
		kcadm) cmd_kcadm "$@" ;;
		*) die "unknown command: ${cmd} (run with help)" ;;
	esac
}

main "$@"
