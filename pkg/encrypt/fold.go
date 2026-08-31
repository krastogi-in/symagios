package encrypt

import (
	"fmt"
	"strings"
)

// FoldResultIntoPayload decrypts payload + result, appends the result as a new
// summary line into the payload history, re-encrypts, and clears result.
// Call on every fetch so prior context is kept while the latest outcome is folded in.
// When result is empty, returns the inputs unchanged (changed=false).
func FoldResultIntoPayload(key *EncryptKey, payload, result string) (newPayload, newResult string, changed bool, err error) {
	if strings.TrimSpace(result) == "" {
		return payload, result, false, nil
	}
	prev := strings.TrimSpace(DecryptStored(key, payload))
	outcome := strings.TrimSpace(DecryptStored(key, result))
	merged := prev
	if outcome != "" {
		if merged != "" {
			merged += "\n\n"
		}
		merged += outcome
	}
	merged = strings.TrimSpace(merged)
	if key == nil {
		return merged, "", true, nil
	}
	enc, err := key.Encrypt(merged)
	if err != nil {
		return payload, result, false, fmt.Errorf("encrypt folded payload: %w", err)
	}
	return enc, "", true, nil
}

// DecryptStored is safe on a nil key (returns value unchanged).
func DecryptStored(key *EncryptKey, value string) string {
	if key == nil {
		return value
	}
	return key.DecryptStored(value)
}
