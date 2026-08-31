#!/bin/sh
# Read Keycloak secrets from Vault over mTLS and write them for the Keycloak entrypoint.
set -eu

out="${SECRET_DIR:-/out}"
i=0
while [ "$i" -lt 90 ]; do
	if vault kv get -field=client_secret secret/symagios/keycloak/clients/symagios-gateway > "${out}/client_secret.tmp" \
		&& vault kv get -field=password secret/symagios/keycloak/admin > "${out}/admin_password.tmp"; then
		mv "${out}/client_secret.tmp" "${out}/client_secret"
		mv "${out}/admin_password.tmp" "${out}/admin_password"
		exit 0
	fi
	i=$((i + 1))
	sleep 2
done
echo "vault mTLS read failed" >&2
exit 1
