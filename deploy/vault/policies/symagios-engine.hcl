# Engine: encryption key + LLM API key only.
path "secret/data/symagios/engine" {
  capabilities = ["read"]
}

path "secret/data/symagios/llm" {
  capabilities = ["read"]
}
