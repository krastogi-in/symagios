#!/usr/bin/env bash
# Bring up the full Symagios secured stack (auth mesh + workloads).
#
# Usage:
#   ./scripts/stack-start.sh              # auth if needed, then all auth services
#   ./scripts/stack-start.sh --watch      # also start HITL/action watcher
#   ./scripts/stack-start.sh --build      # rebuild images before up
#   ./scripts/stack-start.sh --auth-force # always re-run make auth-install
#   ./scripts/stack-start.sh --skip-auth  # compose up only (Vault already seeded)
#
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
# shellcheck disable=SC1091
source ./scripts/lib/load-env.sh
# shellcheck disable=SC1091
source ./scripts/lib/runtime.sh

WITH_WATCH=0
DO_BUILD=0
AUTH_FORCE=0
SKIP_AUTH=0

usage() {
	sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'
	exit "${1:-0}"
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--watch) WITH_WATCH=1 ;;
	--build) DO_BUILD=1 ;;
	--auth-force) AUTH_FORCE=1 ;;
	--skip-auth) SKIP_AUTH=1 ;;
	-h | --help) usage 0 ;;
	*)
		echo "unknown flag: $1" >&2
		usage 1
		;;
	esac
	shift
done

log() { echo "[stack-start] $*"; }

# Vertex auth: prefer Application Default Credentials (refreshing) over
# VERTEX_ACCESS_TOKEN (expires ~1h and is frozen into the container env).
ADC_HOST="${VERTEX_ADC_HOST:-${HOME}/.config/gcloud/application_default_credentials.json}"
export VERTEX_ADC_HOST="$ADC_HOST"
if [[ -f "$ADC_HOST" ]]; then
	export GOOGLE_APPLICATION_CREDENTIALS="${GOOGLE_APPLICATION_CREDENTIALS:-/gcp/adc.json}"
	# Drop stale one-shot bearer so the engine uses ADC inside the container.
	unset VERTEX_ACCESS_TOKEN || true
	log "using Vertex ADC from $ADC_HOST (no VERTEX_ACCESS_TOKEN refresh needed)"
elif [[ "${SYMAGIOS_VERTEX_FORCE_TOKEN:-}" == "1" ]] || [[ -n "${VERTEX_ACCESS_TOKEN:-}" ]]; then
	log "ADC file missing at $ADC_HOST — using VERTEX_ACCESS_TOKEN (expires ~1h)"
	if [[ -z "${VERTEX_ACCESS_TOKEN:-}" ]] && command -v gcloud >/dev/null 2>&1; then
		export VERTEX_ACCESS_TOKEN="$(gcloud auth print-access-token)"
		log "refreshed VERTEX_ACCESS_TOKEN from gcloud"
	fi
else
	log "no ADC at $ADC_HOST — run: gcloud auth application-default login"
	log "falling back to gcloud print-access-token for this start only"
	if command -v gcloud >/dev/null 2>&1; then
		export VERTEX_ACCESS_TOKEN="$(gcloud auth print-access-token)"
	fi
fi
export SYMAGIOS_CONTENT_MODE="${SYMAGIOS_CONTENT_MODE:-redact}"

vault_healthy() {
	symagios_auth_compose exec -T vault \
		wget -qO- http://127.0.0.1:8200/v1/sys/health >/dev/null 2>&1
}

# Dev Vault is in-memory: process restart keeps /sys/health green but wipes KV and
# invalidates VAULT_TOKEN_* in .env. Health-only checks then leave keycloak-vault-fetch
# retrying 403s for minutes. Require a seeded path + a real Keycloak workload token.
vault_seeded() {
	[[ -n "${VAULT_TOKEN:-}" ]] || return 1
	[[ -n "${VAULT_TOKEN_KEYCLOAK:-}" && "${VAULT_TOKEN_KEYCLOAK}" != "pending" ]] || return 1
	symagios_auth_compose exec -T \
		-e VAULT_ADDR=http://127.0.0.1:8200 \
		-e VAULT_TOKEN="${VAULT_TOKEN}" \
		vault vault kv get -field=password secret/symagios/keycloak/admin >/dev/null 2>&1
}

reload_env() {
	# install-vault rewrites .env; shell exports from the initial source would otherwise
	# win over the file and feed stale VAULT_TOKEN_* into compose interpolation.
	# shellcheck disable=SC1091
	source ./scripts/lib/load-env.sh
}

NEED_AUTH=0
if [[ "$SKIP_AUTH" -eq 0 ]]; then
	if [[ "$AUTH_FORCE" -eq 1 ]]; then
		NEED_AUTH=1
	elif ! vault_healthy; then
		log "Vault not healthy — will run auth-install"
		NEED_AUTH=1
	elif ! vault_seeded; then
		log "Vault healthy but KV/tokens missing or stale — will re-seed (dev Vault lost state)"
		NEED_AUTH=1
	else
		log "Vault seeded — skipping auth-install (use --auth-force to re-seed)"
	fi

	if [[ "$NEED_AUTH" -eq 1 ]]; then
		log "running auth-install (Vault / certs / Keycloak / OPA / Envoy)"
		./scripts/install-auth.sh || {
			log "auth-install reported an error — checking whether services recovered…"
			reload_env
			vault_seeded || {
				echo "Vault is not seeded after auth-install; aborting." >&2
				echo "Try: ./scripts/stack-start.sh --auth-force" >&2
				exit 1
			}
			log "Vault seeded; continuing (re-run install-keycloak/envoy if edge auth fails)"
		}
		reload_env
	fi
else
	log "skipping auth-install (--skip-auth)"
fi

UP_ARGS=(--profile auth up -d)
if [[ "$DO_BUILD" -eq 1 ]]; then
	UP_ARGS+=(--build)
fi

log "starting auth-profile services"
symagios_compose "${UP_ARGS[@]}"
# After re-seed: refresh the secrets volume via run --rm (never `up` the one-shot —
# that left Exited(1) and blocked Keycloak on service_completed_successfully), ensure
# Keycloak is up, then sync RBAC. No force-recreate unless Keycloak is missing.
if [[ "$NEED_AUTH" -eq 1 ]]; then
	log "refreshing Keycloak secrets volume from Vault"
	symagios_keycloak_vault_fetch
	symagios_compose --profile auth up -d --no-deps keycloak
	log "bootstrapping Keycloak client secret + RBAC users from Vault"
	./scripts/install-keycloak.sh --sync-only
fi

if [[ "$WITH_WATCH" -eq 1 ]]; then
	log "starting watch profile (--no-recreate so engine stays put)"
	symagios_compose --profile auth --profile watch up -d --no-recreate watch
fi

log "done"
symagios_compose --profile auth ps
if [[ "$WITH_WATCH" -eq 1 ]]; then
	symagios_compose --profile auth --profile watch ps watch 2>/dev/null || true
fi
echo
log "next: source .env && TOKEN=\$(./scripts/keycloak-admin.sh token \"\$SYMAGIOS_OPERATOR_USER\" \"\$SYMAGIOS_OPERATOR_PASSWORD\" | jq -r .access_token)"
log "upload: see HOW_TO_EXECUTE.md §3"
