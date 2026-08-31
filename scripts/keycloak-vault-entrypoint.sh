#!/bin/bash
# Keycloak container entrypoint.
# The gateway client secret is read from a file written by keycloak-vault-fetch,
# which talks to Vault over mTLS. This process does not open an HTTP connection to Vault.
set -euo pipefail

SECRET_DIR="${SECRET_DIR:-/run/secrets/vault}"

log() {
	echo "[keycloak-vault] $*"
}

wait_for_file() {
	local path="$1"
	local i
	for i in $(seq 1 90); do
		if [[ -s "$path" ]]; then
			return 0
		fi
		log "waiting for ${path}"
		sleep 2
	done
	return 1
}

wait_for_local() {
	local i
	for i in $(seq 1 90); do
		if exec 3<>/dev/tcp/127.0.0.1/8080; then
			exec 3<&- || true
			return 0
		fi
		sleep 2
	done
	return 1
}

wait_for_file "${SECRET_DIR}/client_secret"
wait_for_file "${SECRET_DIR}/admin_password"

client_secret=$(cat "${SECRET_DIR}/client_secret")
export KEYCLOAK_ADMIN_PASSWORD
KEYCLOAK_ADMIN_PASSWORD=$(cat "${SECRET_DIR}/admin_password")

log "starting Keycloak"
/opt/keycloak/bin/kc.sh start-dev --import-realm &
kc_pid=$!

log "waiting for Keycloak on 127.0.0.1:8080"
if ! wait_for_local; then
	kill "$kc_pid" || true
	exit 1
fi

log "setting symagios-gateway client secret from Vault"
/opt/keycloak/bin/kcadm.sh config credentials \
	--server http://127.0.0.1:8080 \
	--realm master \
	--user "${KEYCLOAK_ADMIN:-admin}" \
	--password "${KEYCLOAK_ADMIN_PASSWORD}" >/dev/null

client_uuid=$(/opt/keycloak/bin/kcadm.sh get clients -r symagios -q clientId=symagios-gateway --fields id \
	| sed -n 's/.*"id" *: *"\([^"]*\)".*/\1/p' | head -n 1)

if [[ -z "${client_uuid}" ]]; then
	log "symagios-gateway client was not imported"
	kill "$kc_pid" || true
	exit 1
fi

/opt/keycloak/bin/kcadm.sh update "clients/${client_uuid}" -r symagios -s "secret=${client_secret}" >/dev/null
log "client secret applied"

wait "$kc_pid"
