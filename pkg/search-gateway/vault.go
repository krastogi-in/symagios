package searchgateway

import (
	"context"
	"fmt"

	"symagios/vault"
)

func resolveAPIKey(ctx context.Context, cfg Config) (string, error) {
	if cfg.APIKey != "" {
		return cfg.APIKey, nil
	}
	key, err := vault.GetField(ctx, vault.Config{
		Addr:       cfg.VaultAddr,
		Token:      cfg.VaultToken,
		CACert:     cfg.ClientCAFile,
		ClientCert: cfg.ClientCertFile,
		ClientKey:  cfg.ClientKeyFile,
		Path:       cfg.VaultKeyPath,
	}, "api_key")
	if err != nil {
		return "", fmt.Errorf("tavily api key from vault: %w", err)
	}
	return key, nil
}
