#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "${ROOT}/pkg"
protoc \
  --proto_path="${ROOT}/proto" \
  --go_out=. \
  --go_opt=module=symagios \
  "${ROOT}/proto/agent_manifest.proto" \
  "${ROOT}/proto/a2a.proto"

echo "Generated: pkg/proto/manifest/agent_manifest.pb.go"
echo "Generated: pkg/proto/a2a/a2a.pb.go"
