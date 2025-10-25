// Package crypto provides cryptographic primitives for the Connektn Linker Agent.
// It implements deterministic HMAC-based synthetic ID generation and link proof
// creation for privacy-preserving data reconciliation.
//
// Security: All functions treat inputs as sensitive. Error messages NEVER include
// salt values, raw identifiers, or any derived hashes to prevent accidental leakage.
//
// Threat model: Assumes tenantSalt is securely generated (crypto/rand), properly
// stored (secrets management), and rotated periodically. The salt MUST NOT be
// logged, printed, or transmitted outside the agent.
package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
)

var (
	// ErrEmptySalt is returned when the tenant salt is nil or empty.
	ErrEmptySalt = errors.New("crypto: tenant salt must not be empty")

	// ErrEmptyInput is returned when the raw input string is empty.
	ErrEmptyInput = errors.New("crypto: input must not be empty")

	// ErrInvalidHex is returned when a hex string cannot be decoded.
	ErrInvalidHex = errors.New("crypto: invalid hex encoding")

	// ErrEmptyInputs is returned when no inputs are provided to LinkProofHex.
	ErrEmptyInputs = errors.New("crypto: at least one input required")
)

// HMACSHA256Hex computes HMAC-SHA256(tenantSalt, raw) and returns lowercase hex.
// The tenantSalt is provided by the tenant and MUST NOT be logged or printed.
//
// This function is used to create deterministic synthetic identifiers from raw IDs.
// The same salt + same input will always produce the same output, enabling
// privacy-preserving joins across data sources.
//
// Parameters:
//   - tenantSalt: cryptographic salt unique to this tenant (must not be empty)
//   - raw: the raw identifier to hash (e.g., customer ID, email hash)
//
// Returns:
//   - string: lowercase hex-encoded HMAC (64 characters for SHA-256)
//   - error: ErrEmptySalt if salt is nil/empty, ErrEmptyInput if raw is empty
//
// Security: The salt is the secret that prevents rainbow table attacks.
// Never log the salt or include it in error messages.
func HMACSHA256Hex(tenantSalt []byte, raw string) (string, error) {
	if len(tenantSalt) == 0 {
		return "", ErrEmptySalt
	}
	if raw == "" {
		return "", ErrEmptyInput
	}

	h := hmac.New(sha256.New, tenantSalt)
	h.Write([]byte(raw))
	hash := h.Sum(nil)

	return hex.EncodeToString(hash), nil
}

// LinkProofHex produces a stable SHA-256 hex hash over the provided inputs,
// joined with a non-colliding separator. This is used to prove reconciliation
// inputs and recipe versions without exposing PII.
//
// The inputs are joined with the ASCII Unit Separator (0x1F) to prevent
// collision attacks (e.g., ["ab", "c"] vs ["a", "bc"]).
//
// Parameters:
//   - inputs: ordered list of strings to hash (e.g., synthetic IDs, recipe version)
//
// Returns:
//   - string: lowercase hex-encoded SHA-256 hash (64 characters)
//   - error: ErrEmptyInputs if no inputs are provided
//
// Example:
//
//	proof, _ := LinkProofHex("syn_cust_abc", "syn_inv_123", "recipe_v1")
//	// Returns deterministic hash of these three inputs
//
// Security: Input order matters. ["a", "b"] != ["b", "a"].
// This ensures callers cannot reorder inputs to forge proofs.
func LinkProofHex(inputs ...string) (string, error) {
	if len(inputs) == 0 {
		return "", ErrEmptyInputs
	}

	// Use ASCII Unit Separator (0x1F) as delimiter to prevent collisions
	const separator = "\x1f"
	joined := strings.Join(inputs, separator)

	h := sha256.New()
	h.Write([]byte(joined))
	hash := h.Sum(nil)

	return hex.EncodeToString(hash), nil
}

// ConstantTimeEqualHex compares two hex-encoded strings in constant time.
// This prevents timing attacks when comparing sensitive values like proofs or MACs.
//
// Both strings must be valid lowercase hex. If either string contains invalid
// hex characters, the function returns (false, ErrInvalidHex).
//
// Parameters:
//   - a, b: hex-encoded strings to compare
//
// Returns:
//   - bool: true if the decoded byte arrays are equal
//   - error: ErrInvalidHex if either string is not valid hex
//
// Security: Uses crypto/subtle.ConstantTimeCompare to prevent timing side-channels.
// Always check both return values before trusting the result.
func ConstantTimeEqualHex(a, b string) (bool, error) {
	// Decode both hex strings
	aBytes, err := hex.DecodeString(a)
	if err != nil {
		return false, ErrInvalidHex
	}

	bBytes, err := hex.DecodeString(b)
	if err != nil {
		return false, ErrInvalidHex
	}

	// subtle.ConstantTimeCompare returns 1 if equal, 0 otherwise
	return subtle.ConstantTimeCompare(aBytes, bBytes) == 1, nil
}
