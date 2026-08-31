#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="${root}/ebpf/headers/vmlinux.h"

if ! command -v bpftool >/dev/null; then
	echo "bpftool not found; install bpftool (e.g. dnf install bpftool)" >&2
	exit 1
fi

if [[ ! -r /sys/kernel/btf/vmlinux ]]; then
	echo "/sys/kernel/btf/vmlinux not readable; kernel BTF is required" >&2
	exit 1
fi

mkdir -p "$(dirname "$out")"
bpftool btf dump file /sys/kernel/btf/vmlinux format c >"$out"
echo "wrote $out"
