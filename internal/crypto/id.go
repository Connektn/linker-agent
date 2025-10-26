package crypto

import (
	"fmt"
)

// SyntheticID returns a prefixed, lowercase-hex HMAC over the raw ID.
// Example: SyntheticID(salt, "cus_123", "syn_cust") => "syn_cust_92f6ab3e10"
// The output length is truncated to 16 hex chars for collision resistance at scale.
//
// Threat model:
// - Deterministic per tenant: same raw ID always produces same synthetic ID
// - Unguessable across tenants: different salts produce different outputs
// - Irreversible: cannot derive raw ID from synthetic ID (HMAC property)
// - Collision resistant: 16 hex chars (64 bits) provides sufficient uniqueness
func SyntheticID(tenantSalt []byte, raw, prefix string) (string, error) {
	h, err := HMACSHA256Hex(tenantSalt, raw)
	if err != nil {
		return "", err
	}
	if len(h) > 16 {
		h = h[:16]
	}
	return fmt.Sprintf("%s_%s", prefix, h), nil
}
