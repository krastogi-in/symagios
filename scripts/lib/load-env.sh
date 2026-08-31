#!/usr/bin/env bash
# shellcheck shell=bash
# Source from other scripts after cd to repo root.
if [[ -f .env ]]; then
	set -a
	# shellcheck disable=SC1091
	source .env
	set +a
fi
