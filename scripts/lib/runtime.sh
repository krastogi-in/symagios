#!/usr/bin/env bash
# Shared container runtime for Symagios install/admin scripts.
# Default: Podman (rootless API socket). Override with SYMAGIOS_CONTAINER_RUNTIME=docker.
#
# shellcheck shell=bash

# COMPOSE: array — ("podman" "compose") or ("docker" "compose")
# SYMAGIOS_CONTAINER_BIN: podman | docker

symagios_runtime_init() {
	if [[ -n "${_SYMAGIOS_RUNTIME_INIT:-}" ]]; then
		return 0
	fi
	_SYMAGIOS_RUNTIME_INIT=1

	local prefer="${SYMAGIOS_CONTAINER_RUNTIME:-podman}"
	export PODMAN_COMPOSE_WARNING_LOGS="${PODMAN_COMPOSE_WARNING_LOGS:-false}"

	case "${prefer}" in
	podman)
		if ! command -v podman >/dev/null 2>&1; then
			echo "podman is required (or set SYMAGIOS_CONTAINER_RUNTIME=docker)" >&2
			return 1
		fi
		if [[ -z "${XDG_RUNTIME_DIR:-}" ]]; then
			export XDG_RUNTIME_DIR="/run/user/$(id -u)"
		fi
		_symagios_ensure_podman_socket || return 1
		export DOCKER_HOST="unix://${XDG_RUNTIME_DIR}/podman/podman.sock"
		SYMAGIOS_CONTAINER_BIN=podman
		COMPOSE=(podman compose)
		;;
	docker)
		if ! command -v docker >/dev/null 2>&1; then
			echo "docker is required (or set SYMAGIOS_CONTAINER_RUNTIME=podman)" >&2
			return 1
		fi
		SYMAGIOS_CONTAINER_BIN=docker
		COMPOSE=(docker compose)
		;;
	*)
		echo "unknown SYMAGIOS_CONTAINER_RUNTIME=${prefer} (use podman or docker)" >&2
		return 1
		;;
	esac
	return 0
}

_symagios_ensure_podman_socket() {
	local sock="${XDG_RUNTIME_DIR}/podman/podman.sock"
	if [[ -S "${sock}" ]]; then
		return 0
	fi
	mkdir -p "${XDG_RUNTIME_DIR}/podman" 2>/dev/null || true
	if command -v systemctl >/dev/null 2>&1; then
		systemctl --user enable --now podman.socket >/dev/null 2>&1 || true
	fi
	# Wait briefly for systemd socket activation.
	local i
	for i in 1 2 3 4 5; do
		if [[ -S "${sock}" ]]; then
			return 0
		fi
		sleep 0.4
	done
	# Fallback: transient API service (survives for this session).
	if [[ ! -S "${sock}" ]]; then
		podman system service --time=0 "unix://${sock}" >/dev/null 2>&1 &
		disown || true
		for i in 1 2 3 4 5 6 7 8 9 10; do
			if [[ -S "${sock}" ]]; then
				return 0
			fi
			sleep 0.3
		done
	fi
	if [[ ! -S "${sock}" ]]; then
		echo "podman API socket missing at ${sock}" >&2
		echo "try: systemctl --user enable --now podman.socket" >&2
		return 1
	fi
	return 0
}

symagios_require_compose() {
	symagios_runtime_init || exit 1
	if ! "${COMPOSE[@]}" version >/dev/null 2>&1; then
		echo "${COMPOSE[*]} is required (install docker-compose plugin or podman-compose)" >&2
		exit 1
	fi
}

# Run compose with auth profile (most install scripts).
symagios_auth_compose() {
	symagios_require_compose
	"${COMPOSE[@]}" --profile auth "$@"
}

symagios_compose() {
	symagios_require_compose
	"${COMPOSE[@]}" "$@"
}

symagios_build() {
	symagios_runtime_init || exit 1
	"${SYMAGIOS_CONTAINER_BIN}" build "$@"
}
