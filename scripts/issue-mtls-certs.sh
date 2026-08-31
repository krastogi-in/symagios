#!/usr/bin/env bash
# Issue Vault PKI certs for Symagios container mTLS into deploy/certs/.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
# shellcheck disable=SC1091
source ./scripts/lib/load-env.sh
# shellcheck disable=SC1091
source ./scripts/lib/runtime.sh

VAULT_ADDR="${VAULT_ADDR:-http://localhost:8200}"
: "${VAULT_TOKEN:?VAULT_TOKEN missing — run ./scripts/generate-dev-secrets.sh}"
PKI_ROLE="${PKI_ROLE:-symagios-workload}"
TTL="${MTLS_TTL:-72h}"
CERT_DIR="${CERT_DIR:-deploy/certs}"

WORKLOADS=(
	"envoy-ingress"
	"envoy-egress"
	"agent-gateway"
	"agent-entry"
	"mcp-gateway"
	"engine"
	"search-broker"
	"opa"
	"keycloak"
	"vault"
)

log() {
	echo "[issue-mtls-certs] $*"
}

die() {
	echo "[issue-mtls-certs] error: $*" >&2
	exit 1
}

vault_cli() {
	symagios_auth_compose exec -T \
		-e VAULT_ADDR=http://127.0.0.1:8200 \
		-e VAULT_TOKEN="${VAULT_TOKEN}" \
		vault vault "$@"
}

ensure_vault() {
	symagios_auth_compose exec -T vault \
		wget -qO- http://127.0.0.1:8200/v1/sys/health >/dev/null 2>&1 || \
		die "vault is not running; run: make vault-up"
}

ensure_pki_role() {
	log "ensuring PKI role ${PKI_ROLE}"
	if ! vault_cli secrets list -format=json | jq -e '.["pki/"]' >/dev/null 2>&1; then
		vault_cli secrets enable pki
		vault_cli secrets tune -max-lease-ttl=87600h pki
		vault_cli write -format=json pki/root/generate/internal \
			common_name=symagios.local ttl=87600h >/dev/null
		vault_cli write pki/config/urls \
			issuing_certificates="http://vault:8200/v1/pki/ca" \
			crl_distribution_points="http://vault:8200/v1/pki/crl"
	fi

	# Compose DNS short names (agent-gateway) + FQDNs (*.symagios.local).
	vault_cli write "pki/roles/${PKI_ROLE}" \
		allowed_domains=symagios.local \
		allow_subdomains=true \
		allow_bare_domains=true \
		allow_any_name=true \
		allow_localhost=true \
		allow_ip_sans=true \
		max_ttl=72h \
		key_type=rsa \
		key_bits=2048 \
		client_flag=true \
		server_flag=true >/dev/null
}

write_ca() {
	mkdir -p "${CERT_DIR}"
	vault_cli read -field=certificate pki/cert/ca > "${CERT_DIR}/ca.crt"
	chmod 644 "${CERT_DIR}/ca.crt"
	log "wrote ${CERT_DIR}/ca.crt"
}

issue_one() {
	local name="$1"
	local cn="${name}.symagios.local"
	local out_dir="${CERT_DIR}/${name}"
	mkdir -p "${out_dir}"

	# Compose DNS + local HTTPS edge (curl https://localhost:8443)
	if [[ "${name}" == "envoy-ingress" ]]; then
		alt_names="${name},${cn},localhost"
		ip_sans="127.0.0.1"
	else
		alt_names="${name},${cn}"
		ip_sans=""
	fi

	log "issuing cert for ${cn} (SAN: ${alt_names})"
	local json
	if [[ -n "${ip_sans}" ]]; then
		json="$(vault_cli write -format=json "pki/issue/${PKI_ROLE}" \
			common_name="${cn}" \
			alt_names="${alt_names}" \
			ip_sans="${ip_sans}" \
			ttl="${TTL}")"
	else
		json="$(vault_cli write -format=json "pki/issue/${PKI_ROLE}" \
			common_name="${cn}" \
			alt_names="${alt_names}" \
			ttl="${TTL}")"
	fi

	echo "${json}" | jq -r '.data.certificate' > "${out_dir}/tls.crt"
	echo "${json}" | jq -r '.data.private_key' > "${out_dir}/tls.key"
	echo "${json}" | jq -r '.data.issuing_ca' > "${out_dir}/ca.crt"
	# Readable by non-root container users (Keycloak uid 1000, Envoy, etc.).
	chmod 644 "${out_dir}/tls.crt" "${out_dir}/ca.crt" "${out_dir}/tls.key"
	log "wrote ${out_dir}/tls.crt"
}

main() {
	command -v jq >/dev/null 2>&1 || die "jq is required"
	command -v curl >/dev/null 2>&1 || die "curl is required"
	symagios_require_compose

	# Compose creates empty directories when bind-mounting missing cert files.
	# Those stubs break later mounts ("ca.crt is a directory").
	mkdir -p "${CERT_DIR}"
	for stub in ca.crt ca.p12; do
		if [[ -d "${CERT_DIR}/${stub}" ]]; then
			rmdir "${CERT_DIR}/${stub}" 2>/dev/null || \
				die "${CERT_DIR}/${stub} is a directory (compose mount stub); remove it and re-run"
		fi
	done

	ensure_vault
	ensure_pki_role
	write_ca

	for w in "${WORKLOADS[@]}"; do
		issue_one "${w}"
	done

	if command -v keytool >/dev/null 2>&1; then
		log "writing Keycloak client-auth truststore (Java keytool)"
		rm -f "${CERT_DIR}/ca.p12"
		# openssl pkcs12 -nokeys can produce a store Java lists as 0 entries;
		# keytool imports the CA as a trustedCertEntry Keycloak can load.
		keytool -importcert -noprompt -alias symagios-ca \
			-file "${CERT_DIR}/ca.crt" \
			-keystore "${CERT_DIR}/ca.p12" \
			-storetype PKCS12 \
			-storepass changeit
		chmod 644 "${CERT_DIR}/ca.p12"
	elif command -v openssl >/dev/null 2>&1; then
		die "keytool is required to build ${CERT_DIR}/ca.p12 for Keycloak (Java); openssl-only packs are often empty to keytool"
	else
		die "keytool is required to build ${CERT_DIR}/ca.p12 for Keycloak mTLS"
	fi

	cat <<EOF

mTLS certs issued under ${CERT_DIR}/

CA : ${CERT_DIR}/ca.crt
Workloads: ${WORKLOADS[*]}

  Next:
  make mtls-up
  # Restarts envoy-ingress (deploy/envoy/envoy.yaml = edge TLS + upstream mTLS)

Agent gateway must listen TLS on 8443 with:
  cert: ${CERT_DIR}/agent-gateway/tls.crt
  key:  ${CERT_DIR}/agent-gateway/tls.key
  ca:   ${CERT_DIR}/ca.crt  (require client certs)

Agent entry (TUS) must listen TLS on 8443 with:
  cert: ${CERT_DIR}/agent-entry/tls.crt
  key:  ${CERT_DIR}/agent-entry/tls.key
  ca:   ${CERT_DIR}/ca.crt  (require client certs)

MCP gateway must listen TLS on 8443 with:
  cert: ${CERT_DIR}/mcp-gateway/tls.crt
  key:  ${CERT_DIR}/mcp-gateway/tls.key
  ca:   ${CERT_DIR}/ca.crt  (require client certs)

Config: deploy/docs/mtls.md

EOF
}

main "$@"
