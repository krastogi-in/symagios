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

vault_ok() {
	symagios_auth_compose exec -T vault \
		wget -qO- http://127.0.0.1:8200/v1/sys/health >/dev/null 2>&1
}

if [[ "$SKIP_AUTH" -eq 0 ]]; then
	if [[ "$AUTH_FORCE" -eq 1 ]] || ! vault_ok; then
		log "running auth-install (Vault / certs / Keycloak / OPA / Envoy)"
		./scripts/install-auth.sh || {
			log "auth-install reported an error — checking whether services recovered…"
			# Keycloak/Envoy waits are flaky on cold start; continue if Vault is up.
			vault_ok || {
				echo "Vault is not healthy after auth-install; aborting." >&2
				exit 1
			}
			log "Vault healthy; continuing (re-run install-keycloak/envoy if edge auth fails)"
		}
	else
		log "Vault already healthy — skipping auth-install (use --auth-force to re-seed)"
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
