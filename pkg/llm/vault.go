package llm

import (
	"context"

	"symagios/vault"
)

func apiKeyFromVault(ctx context.Context, cfg Config) (string, error) {
	v := vault.ConfigFromEnv()
	v.Path = cfg.VaultPath
	if cfg.VaultField == "" {
		cfg.VaultField = "api_key"
	}
	return vault.GetField(ctx, v, cfg.VaultField)
}
