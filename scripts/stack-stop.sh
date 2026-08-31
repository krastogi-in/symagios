#!/usr/bin/env bash
# Stop Symagios compose services (auth + watch). Does not delete volumes/DB.
#
# Usage:
#   ./scripts/stack-stop.sh
#   ./scripts/stack-stop.sh --purge-orphans   # also remove leftover one-shot containers
#
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
# shellcheck disable=SC1091
source ./scripts/lib/load-env.sh
# shellcheck disable=SC1091
source ./scripts/lib/runtime.sh

PURGE_ORPHANS=0

usage() {
	sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//'
	exit "${1:-0}"
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--purge-orphans) PURGE_ORPHANS=1 ;;
	-h | --help) usage 0 ;;
	*)
		echo "unknown flag: $1" >&2
		usage 1
		;;
	esac
	shift
done

log() { echo "[stack-stop] $*"; }

symagios_require_compose

log "stopping auth + watch profiles"
symagios_compose --profile auth --profile watch down --remove-orphans || true

if [[ "$PURGE_ORPHANS" -eq 1 ]]; then
	log "removing leftover symagios containers (one-shot watch runs, etc.)"
	# Names like symagios-watch-run-* from compose run
	mapfile -t leftover < <("${SYMAGIOS_CONTAINER_BIN}" ps -aq --filter "name=symagios" 2>/dev/null || true)
	if [[ ${#leftover[@]} -gt 0 ]]; then
		"${SYMAGIOS_CONTAINER_BIN}" rm -f "${leftover[@]}" >/dev/null 2>&1 || true
		log "removed ${#leftover[@]} container(s)"
	else
		log "no leftover containers"
	fi
fi

log "stopped (volumes kept — use ./scripts/stack-delete.sh to wipe DB)"
