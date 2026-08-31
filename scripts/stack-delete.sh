#!/usr/bin/env bash
# Stop the stack and delete persisted data volumes.
#
# Usage:
#   ./scripts/stack-delete.sh              # wipe SQLite volume (symagios-data) only
#   ./scripts/stack-delete.sh --all        # also Keycloak + vault-fetch secret volumes
#   ./scripts/stack-delete.sh --yes        # skip confirmation
#
# Vault -dev state lives in the container filesystem (lost on recreate anyway).
# After --all you typically need: ./scripts/stack-start.sh --auth-force
#
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
# shellcheck disable=SC1091
source ./scripts/lib/load-env.sh
# shellcheck disable=SC1091
source ./scripts/lib/runtime.sh

DELETE_ALL=0
ASSUME_YES=0

usage() {
	sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'
	exit "${1:-0}"
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--all) DELETE_ALL=1 ;;
	--yes | -y) ASSUME_YES=1 ;;
	-h | --help) usage 0 ;;
	*)
		echo "unknown flag: $1" >&2
		usage 1
		;;
	esac
	shift
done

log() { echo "[stack-delete] $*"; }

symagios_require_compose

PROJECT="$("${COMPOSE[@]}" --profile auth config --format json 2>/dev/null | jq -r '.name // empty' || true)"
if [[ -z "$PROJECT" ]]; then
	PROJECT="$(basename "$ROOT_DIR" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9_-')"
fi

VOL_DB="${PROJECT}_symagios-data"
VOL_KC="${PROJECT}_keycloak-data"
VOL_SECRETS="${PROJECT}_keycloak-vault-secrets"

vols=("$VOL_DB")
if [[ "$DELETE_ALL" -eq 1 ]]; then
	vols+=("$VOL_KC" "$VOL_SECRETS")
fi

echo "Will stop the stack and delete volume(s):"
printf '  - %s\n' "${vols[@]}"
if [[ "$ASSUME_YES" -ne 1 ]]; then
	read -r -p "Continue? [y/N] " ans
	case "$ans" in
	y | Y | yes | YES) ;;
	*)
		log "aborted"
		exit 0
		;;
	esac
fi

log "stopping services"
./scripts/stack-stop.sh --purge-orphans

for v in "${vols[@]}"; do
	if "${SYMAGIOS_CONTAINER_BIN}" volume inspect "$v" >/dev/null 2>&1; then
		log "removing volume $v"
		"${SYMAGIOS_CONTAINER_BIN}" volume rm -f "$v" >/dev/null
	else
		log "volume $v not present (ok)"
	fi
done

log "done"
if [[ "$DELETE_ALL" -eq 1 ]]; then
	log "next: ./scripts/stack-start.sh --auth-force [--watch]"
else
	log "next: ./scripts/stack-start.sh [--watch]   # Vault may still be seeded if containers were only stopped"
	log "      if Vault was recreated empty: ./scripts/stack-start.sh --auth-force"
fi
