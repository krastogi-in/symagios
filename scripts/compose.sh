#!/usr/bin/env bash
# Compose wrapper — Podman by default (SYMAGIOS_CONTAINER_RUNTIME=docker to use Docker).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/lib/runtime.sh"

symagios_require_compose
exec "${COMPOSE[@]}" "$@"
