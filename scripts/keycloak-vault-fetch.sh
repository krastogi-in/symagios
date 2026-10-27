#!/bin/sh
# Read Keycloak secrets from Vault over mTLS and write them for the Keycloak entrypoint.
set -eu

out="${SECRET_DIR:-/out}"
err="${out}/.vault-fetch.err"
i=0
while [ "$i" -lt 90 ]; do
	# Capture stderr: retries help when Vault/mTLS is still coming up, not when the
	# workload token is stale (dev Vault restart) — that is a hard fail.
	if vault kv get -field=client_secret secret/symagios/keycloak/clients/symagios-gateway > "${out}/client_secret.tmp" 2>"${err}" \
		&& vault kv get -field=password secret/symagios/keycloak/admin > "${out}/admin_password.tmp" 2>>"${err}"; then
		mv "${out}/client_secret.tmp" "${out}/client_secret"
		mv "${out}/admin_password.tmp" "${out}/admin_password"
		rm -f "${err}"
		exit 0
	fi
	if grep -qiE 'permission denied|invalid token|code: 403' "${err}" 2>/dev/null; then
		echo "vault mTLS read failed with auth error (stale VAULT_TOKEN_KEYCLOAK or empty KV)." >&2
		echo "Re-seed: ./scripts/stack-start.sh --auth-force   (or make vault-install)" >&2
		cat "${err}" >&2 || true
		exit 1
	fi
	i=$((i + 1))
	sleep 2
done
echo "vault mTLS read failed after retries" >&2
cat "${err}" >&2 || true
exit 1
