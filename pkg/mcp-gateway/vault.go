package mcpgateway

import (
	"context"
	"fmt"
	"strings"

	"symagios/vault"
)

func (s *Service) credential(ctx context.Context, server ServerSpec) (string, error) {
	if server.VaultPath == "" {
		return "", nil
	}
	// Stub servers do not need live Vault for local POC; real upstreams do.
	base := strings.TrimSpace(server.BaseURL)
	if base == "" || strings.HasPrefix(base, "stub://") {
		return "", nil
	}
	cfg := vault.Config{
		Addr:       s.cfg.VaultAddr,
		Token:      s.cfg.VaultToken,
		CACert:     s.cfg.VaultCACert,
		ClientCert: s.cfg.VaultCert,
		ClientKey:  s.cfg.VaultKey,
		Path:       server.VaultPath,
	}
	val, err := vault.GetField(ctx, cfg, server.VaultField)
	if err != nil {
		return "", fmt.Errorf("vault creds for mcp/%s: %w", server.ID, err)
	}
	return val, nil
}
