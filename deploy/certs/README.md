# Issued by: ./scripts/issue-mtls-certs.sh (Vault PKI)
#
# Layout:
#   ca.crt
#   <workload>/tls.crt
#   <workload>/tls.key
#
# Mount into containers as /certs (see docker-compose mtls profile).
