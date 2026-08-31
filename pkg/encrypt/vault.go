package encrypt

import (
	"context"
	"fmt"
	"os"

	"symagios/vault"
)

// KeyFromVaultOrEnv loads the AES-256 key.
// Prefer SYMAGIOS_ENCRYPTION_KEY only for tests; production reads Vault
// secret/symagios/engine field encryption_key.
func KeyFromVaultOrEnv(ctx context.Context) (string, error) {
	if key := os.Getenv("SYMAGIOS_ENCRYPTION_KEY"); key != "" {
		return key, nil
	}
	cfg := vault.ConfigFromEnv()
	key, err := vault.GetField(ctx, cfg, "encryption_key")
	if err != nil {
		return "", fmt.Errorf("load encryption key from vault: %w", err)
	}
	return key, nil
}
