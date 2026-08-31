#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
# shellcheck disable=SC1091
source ./scripts/lib/runtime.sh

WAIT_TIMEOUT="${WAIT_TIMEOUT:-60}"

log() {
	echo "[install-opa] $*"
}

require_command() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "missing required command: $1" >&2
		exit 1
	fi
}

# Distroless OPA image has no wget; binary availability means the server process is up.
wait_for_opa() {
	local deadline=$((SECONDS + WAIT_TIMEOUT))
	while (( SECONDS < deadline )); do
		if symagios_auth_compose exec -T opa /opa version >/dev/null 2>&1; then
			return 0
		fi
		sleep 2
	done
	echo "opa did not become ready within ${WAIT_TIMEOUT}s" >&2
	return 1
}

symagios_require_compose

log "starting opa sidecar"
symagios_auth_compose up -d opa

log "waiting for opa Decision API (loopback inside container)"
wait_for_opa

log "opa is ready"
cat <<EOF

Symagios OPA is running.

Decision API   : 127.0.0.1:8181 inside opa only (not on mesh / host)
Envoy ext_authz: opa:9191 (gRPC on compose network)
Policies       : deploy/opa/policies
Decision path  : data.envoy.authz.allow

Test a policy decision (needs wget — use a debug sidecar, or curl via Envoy once up):
  # Prefer verifying via Envoy ext_authz once ingress is healthy.
  ./scripts/compose.sh --profile auth exec -T opa /opa version

Stop OPA:
  ./scripts/compose.sh --profile auth stop opa

EOF
