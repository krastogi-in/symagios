#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
# shellcheck disable=SC1091
source ./scripts/lib/load-env.sh
# shellcheck disable=SC1091
source ./scripts/lib/runtime.sh

VAULT_ADDR="${VAULT_ADDR:-http://localhost:8200}"
: "${VAULT_TOKEN:?VAULT_TOKEN missing — run ./scripts/generate-dev-secrets.sh}"

usage() {
	cat <<'EOF'
Symagios Vault admin CLI (runs vault inside the Vault container).

Usage:
  ./scripts/vault-admin.sh <command> [args]

Commands:
  status                         Vault health/status
  kv get <path>                  Read KV secret (path without secret/ prefix)
  kv put <path> <k=v> [k=v...]   Write KV secret
  kv list <path>                 List KV keys
  pki issue <common-name>        Issue example workload cert
  pki ca                         Print example CA cert
  exec <args...>                 Run raw vault command

Examples:
  ./scripts/vault-admin.sh kv get symagios/keycloak/clients/symagios-gateway
  ./scripts/vault-admin.sh kv put symagios/keycloak/clients/symagios-gateway client_id=symagios-gateway client_secret=new-secret
  ./scripts/vault-admin.sh pki issue agent-gateway.symagios.local

Environment:
  VAULT_ADDR    Default: http://localhost:8200
  VAULT_TOKEN   Required from .env (no default)

EOF
}

log() {
	echo "[vault-admin] $*" >&2
}

die() {
	log "error: $*"
	exit 1
}

vault_cli() {
	symagios_auth_compose exec -T \
		-e VAULT_ADDR=http://127.0.0.1:8200 \
		-e VAULT_TOKEN="${VAULT_TOKEN}" \
		vault vault "$@"
}

ensure_vault() {
	symagios_auth_compose exec -T vault true >/dev/null 2>&1 || \
		die "vault is not running; run: make vault-up"
}

cmd_status() {
	ensure_vault
	curl -fsS "${VAULT_ADDR}/v1/sys/health" | jq .
}

cmd_kv_get() {
	local path="${1:-}"
	[[ -n "${path}" ]] || die "usage: kv get <path>"
	ensure_vault
	vault_cli kv get -format=json "secret/${path}"
}

cmd_kv_put() {
	local path="${1:-}"
	shift || true
	[[ -n "${path}" && $# -gt 0 ]] || die "usage: kv put <path> key=value [key=value...]"
	ensure_vault
	vault_cli kv put "secret/${path}" "$@"
}

cmd_kv_list() {
	local path="${1:-}"
	ensure_vault
	if [[ -n "${path}" ]]; then
		vault_cli kv list "secret/${path}"
	else
		vault_cli kv list secret/
	fi
}

cmd_pki_issue() {
	local common_name="${1:-}"
	[[ -n "${common_name}" ]] || die "usage: pki issue <common-name>"
	ensure_vault
	vault_cli write -format=json pki/issue/symagios-workload \
		common_name="${common_name}" ttl=24h
}

cmd_pki_ca() {
	ensure_vault
	vault_cli read -format=json pki/cert/ca
}

cmd_exec() {
	[[ $# -gt 0 ]] || die "usage: exec <args...>"
	ensure_vault
	vault_cli "$@"
}

main() {
	local cmd="${1:-help}"
	shift || true

	symagios_require_compose
	command -v curl >/dev/null 2>&1 || die "curl is required"
	command -v jq >/dev/null 2>&1 || die "jq is required"

	case "${cmd}" in
		help|-h|--help) usage ;;
		status) cmd_status ;;
		kv)
			case "${1:-}" in
				get) shift; cmd_kv_get "$@" ;;
				put) shift; cmd_kv_put "$@" ;;
				list) shift; cmd_kv_list "$@" ;;
				*) die "usage: kv get|put|list" ;;
			esac
			;;
		pki)
			case "${1:-}" in
				issue) shift; cmd_pki_issue "$@" ;;
				ca) cmd_pki_ca ;;
				*) die "usage: pki issue|ca" ;;
			esac
			;;
		exec) cmd_exec "$@" ;;
		*) die "unknown command: ${cmd}" ;;
	esac
}

main "$@"
