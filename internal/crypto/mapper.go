// Package crypto provides the IDMapper interface and implementations for
// dual privacy modes: strict (synthetic IDs) and passthrough (raw platform IDs).
package crypto

import (
	"fmt"
	"strings"
)

// IDMode represents the privacy mode for ID handling.
type IDMode string

const (
	// IDModeStrict emits only synthetic IDs (HMAC-based), never raw platform IDs.
	IDModeStrict IDMode = "strict"

	// IDModePassthrough emits raw platform IDs (e.g., Stripe cus_/sub_/in_), but still no PII/PHI.
	IDModePassthrough IDMode = "passthrough"
)

// IDMapper provides privacy-aware ID transformation.
// It supports two modes:
//   - strict: converts raw platform IDs to synthetic IDs using HMAC
//   - passthrough: returns raw platform IDs unchanged
//
// Threat model:
//   - Strict mode: no raw IDs ever leak, only HMAC-derived synthetic IDs
//   - Passthrough mode: raw platform IDs (e.g., Stripe cus_xxx) are preserved,
//     but PII/PHI is still never included
//   - Both modes produce different proofs to prevent cross-mode replay attacks
type IDMapper interface {
	// CustomerID transforms a raw customer ID according to the privacy mode.
	CustomerID(raw string) (string, error)

	// SubscriptionID transforms a raw subscription ID according to the privacy mode.
	SubscriptionID(raw string) (string, error)

	// InvoiceID transforms a raw invoice ID according to the privacy mode.
	InvoiceID(raw string) (string, error)

	// PriceID transforms a raw price ID according to the privacy mode.
	PriceID(raw string) (string, error)

	// ProductID transforms a raw product ID according to the privacy mode.
	ProductID(raw string) (string, error)

	// Mode returns the current privacy mode.
	Mode() IDMode
}

// SyntheticMapper implements IDMapper using HMAC-based synthetic IDs.
// All raw IDs are transformed to "syn_<type>_<short_hash>" format.
//
// Threat model:
//   - Deterministic per tenant: same raw ID always produces same synthetic ID
//   - Unguessable across tenants: different salts produce different outputs
//   - Irreversible: cannot derive raw ID from synthetic ID (HMAC property)
type SyntheticMapper struct {
	tenantSalt []byte
}

// NewSyntheticMapper creates a new SyntheticMapper with the given tenant salt.
// The salt must be non-empty and should be securely generated (crypto/rand).
//
// Returns ErrEmptySalt if the salt is nil or empty.
func NewSyntheticMapper(tenantSalt []byte) (*SyntheticMapper, error) {
	if len(tenantSalt) == 0 {
		return nil, ErrEmptySalt
	}
	return &SyntheticMapper{tenantSalt: tenantSalt}, nil
}

// CustomerID transforms a raw customer ID to a synthetic ID.
func (m *SyntheticMapper) CustomerID(raw string) (string, error) {
	return SyntheticID(m.tenantSalt, raw, "syn_cust")
}

// SubscriptionID transforms a raw subscription ID to a synthetic ID.
func (m *SyntheticMapper) SubscriptionID(raw string) (string, error) {
	return SyntheticID(m.tenantSalt, raw, "syn_sub")
}

// InvoiceID transforms a raw invoice ID to a synthetic ID.
func (m *SyntheticMapper) InvoiceID(raw string) (string, error) {
	return SyntheticID(m.tenantSalt, raw, "syn_inv")
}

// PriceID transforms a raw price ID to a synthetic ID.
func (m *SyntheticMapper) PriceID(raw string) (string, error) {
	return SyntheticID(m.tenantSalt, raw, "syn_price")
}

// ProductID transforms a raw product ID to a synthetic ID.
func (m *SyntheticMapper) ProductID(raw string) (string, error) {
	return SyntheticID(m.tenantSalt, raw, "syn_prod")
}

// Mode returns IDModeStrict.
func (m *SyntheticMapper) Mode() IDMode {
	return IDModeStrict
}

// PassthroughMapper implements IDMapper by returning raw platform IDs unchanged.
// This mode preserves platform IDs (e.g., Stripe cus_xxx, sub_xxx) but still strips PII/PHI.
//
// Threat model:
//   - Raw platform IDs are preserved for external system integration
//   - PII/PHI is still never included (handled by sanitization layer)
//   - Different proof signatures prevent cross-mode replay attacks
type PassthroughMapper struct{}

// NewPassthroughMapper creates a new PassthroughMapper.
func NewPassthroughMapper() *PassthroughMapper {
	return &PassthroughMapper{}
}

// CustomerID returns the raw customer ID unchanged.
func (m *PassthroughMapper) CustomerID(raw string) (string, error) {
	if raw == "" {
		return "", ErrEmptyInput
	}
	return raw, nil
}

// SubscriptionID returns the raw subscription ID unchanged.
func (m *PassthroughMapper) SubscriptionID(raw string) (string, error) {
	if raw == "" {
		return "", ErrEmptyInput
	}
	return raw, nil
}

// InvoiceID returns the raw invoice ID unchanged.
func (m *PassthroughMapper) InvoiceID(raw string) (string, error) {
	if raw == "" {
		return "", ErrEmptyInput
	}
	return raw, nil
}

// PriceID returns the raw price ID unchanged.
func (m *PassthroughMapper) PriceID(raw string) (string, error) {
	if raw == "" {
		return "", ErrEmptyInput
	}
	return raw, nil
}

// ProductID returns the raw product ID unchanged.
func (m *PassthroughMapper) ProductID(raw string) (string, error) {
	if raw == "" {
		return "", ErrEmptyInput
	}
	return raw, nil
}

// Mode returns IDModePassthrough.
func (m *PassthroughMapper) Mode() IDMode {
	return IDModePassthrough
}

// ValidateIDMode checks if the provided mode string is valid.
// Returns an error if the mode is not "strict" or "passthrough".
func ValidateIDMode(mode string) error {
	m := IDMode(mode)
	if m != IDModeStrict && m != IDModePassthrough {
		return fmt.Errorf("invalid id mode %q: must be 'strict' or 'passthrough'", mode)
	}
	return nil
}

// IsStripePlatformID returns true if the ID appears to be a Stripe platform ID.
// This is used for testing to verify that passthrough mode preserves platform IDs
// and strict mode never emits them.
func IsStripePlatformID(id string) bool {
	// Stripe ID prefixes: cus_, sub_, in_, price_, prod_, etc.
	prefixes := []string{"cus_", "sub_", "in_", "price_", "prod_", "pi_", "pm_"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}

// IsSyntheticID returns true if the ID appears to be a synthetic ID.
// Synthetic IDs always start with "syn_".
func IsSyntheticID(id string) bool {
	return strings.HasPrefix(id, "syn_")
}
