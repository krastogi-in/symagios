# keycloak-vault-fetch: Keycloak admin + client secrets.
path "secret/data/symagios/keycloak/*" {
  capabilities = ["read"]
}
