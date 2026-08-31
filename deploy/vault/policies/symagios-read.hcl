# Read Symagios runtime secrets (gateway, engine, keycloak client refs).
path "secret/data/symagios/*" {
  capabilities = ["read"]
}

path "pki/issue/symagios-workload" {
  capabilities = ["create", "update"]
}

path "pki/cert/ca" {
  capabilities = ["read"]
}
