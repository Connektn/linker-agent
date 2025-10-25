package crypto

import (
	"crypto/rand"
	"strings"
	"testing"
)

// TestHMACSHA256Hex_Determinism verifies that identical inputs produce identical outputs.
func TestHMACSHA256Hex_Determinism(t *testing.T) {
	salt := []byte("test-tenant-salt-12345")
	input := "customer_123"

	hash1, err := HMACSHA256Hex(salt, input)
	if err != nil {
		t.Fatalf("HMACSHA256Hex failed: %v", err)
	}

	hash2, err := HMACSHA256Hex(salt, input)
	if err != nil {
		t.Fatalf("HMACSHA256Hex failed on second call: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("HMAC is not deterministic: %s != %s", hash1, hash2)
	}

	// Verify the hash is valid hex and correct length (SHA-256 = 64 hex chars)
	if len(hash1) != 64 {
		t.Errorf("Expected 64 hex characters, got %d", len(hash1))
	}

	// Verify lowercase hex
	if hash1 != strings.ToLower(hash1) {
		t.Errorf("Expected lowercase hex, got %s", hash1)
	}
}

// TestHMACSHA256Hex_SaltIsolation verifies that different salts produce different outputs.
func TestHMACSHA256Hex_SaltIsolation(t *testing.T) {
	salt1 := []byte("tenant-A-salt")
	salt2 := []byte("tenant-B-salt")
	input := "customer_123"

	hash1, err := HMACSHA256Hex(salt1, input)
	if err != nil {
		t.Fatalf("HMACSHA256Hex with salt1 failed: %v", err)
	}

	hash2, err := HMACSHA256Hex(salt2, input)
	if err != nil {
		t.Fatalf("HMACSHA256Hex with salt2 failed: %v", err)
	}

	if hash1 == hash2 {
		t.Errorf("Different salts produced identical hashes (collision): %s", hash1)
	}
}

// TestHMACSHA256Hex_InputIsolation verifies that different inputs produce different outputs.
func TestHMACSHA256Hex_InputIsolation(t *testing.T) {
	salt := []byte("test-salt")
	input1 := "customer_123"
	input2 := "customer_456"

	hash1, err := HMACSHA256Hex(salt, input1)
	if err != nil {
		t.Fatalf("HMACSHA256Hex with input1 failed: %v", err)
	}

	hash2, err := HMACSHA256Hex(salt, input2)
	if err != nil {
		t.Fatalf("HMACSHA256Hex with input2 failed: %v", err)
	}

	if hash1 == hash2 {
		t.Errorf("Different inputs produced identical hashes (collision): %s", hash1)
	}
}

// TestHMACSHA256Hex_EmptySalt verifies that empty salts are rejected.
func TestHMACSHA256Hex_EmptySalt(t *testing.T) {
	tests := []struct {
		name string
		salt []byte
	}{
		{"nil salt", nil},
		{"empty slice", []byte{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := HMACSHA256Hex(tt.salt, "input")
			if err != ErrEmptySalt {
				t.Errorf("Expected ErrEmptySalt, got %v", err)
			}
		})
	}
}

// TestHMACSHA256Hex_EmptyInput verifies that empty inputs are rejected.
func TestHMACSHA256Hex_EmptyInput(t *testing.T) {
	salt := []byte("test-salt")
	_, err := HMACSHA256Hex(salt, "")
	if err != ErrEmptyInput {
		t.Errorf("Expected ErrEmptyInput, got %v", err)
	}
}

// TestLinkProofHex_Stability verifies that identical input orders produce identical outputs.
func TestLinkProofHex_Stability(t *testing.T) {
	inputs := []string{"syn_cust_abc", "syn_inv_123", "recipe_v1"}

	proof1, err := LinkProofHex(inputs...)
	if err != nil {
		t.Fatalf("LinkProofHex failed: %v", err)
	}

	proof2, err := LinkProofHex(inputs...)
	if err != nil {
		t.Fatalf("LinkProofHex failed on second call: %v", err)
	}

	if proof1 != proof2 {
		t.Errorf("LinkProof is not stable: %s != %s", proof1, proof2)
	}

	// Verify the proof is valid hex and correct length (SHA-256 = 64 hex chars)
	if len(proof1) != 64 {
		t.Errorf("Expected 64 hex characters, got %d", len(proof1))
	}

	// Verify lowercase hex
	if proof1 != strings.ToLower(proof1) {
		t.Errorf("Expected lowercase hex, got %s", proof1)
	}
}

// TestLinkProofHex_OrderSensitivity verifies that different input orders produce different outputs.
// This is a security feature: callers cannot reorder inputs to forge proofs.
func TestLinkProofHex_OrderSensitivity(t *testing.T) {
	proof1, err := LinkProofHex("a", "b", "c")
	if err != nil {
		t.Fatalf("LinkProofHex failed: %v", err)
	}

	proof2, err := LinkProofHex("c", "b", "a")
	if err != nil {
		t.Fatalf("LinkProofHex failed on reordered inputs: %v", err)
	}

	if proof1 == proof2 {
		t.Errorf("Reordered inputs produced identical proofs (security violation): %s", proof1)
	}
}

// TestLinkProofHex_SeparatorCollisionPrevention verifies that the separator prevents collisions.
// ["ab", "c"] should produce a different hash than ["a", "bc"].
func TestLinkProofHex_SeparatorCollisionPrevention(t *testing.T) {
	proof1, err := LinkProofHex("ab", "c")
	if err != nil {
		t.Fatalf("LinkProofHex failed: %v", err)
	}

	proof2, err := LinkProofHex("a", "bc")
	if err != nil {
		t.Fatalf("LinkProofHex failed on alternate split: %v", err)
	}

	if proof1 == proof2 {
		t.Errorf("Separator failed to prevent collision: %s", proof1)
	}
}

// TestLinkProofHex_EmptyInputs verifies that no inputs are rejected.
func TestLinkProofHex_EmptyInputs(t *testing.T) {
	_, err := LinkProofHex()
	if err != ErrEmptyInputs {
		t.Errorf("Expected ErrEmptyInputs, got %v", err)
	}
}

// TestLinkProofHex_SingleInput verifies that a single input is accepted.
func TestLinkProofHex_SingleInput(t *testing.T) {
	proof, err := LinkProofHex("single")
	if err != nil {
		t.Fatalf("LinkProofHex failed with single input: %v", err)
	}

	if len(proof) != 64 {
		t.Errorf("Expected 64 hex characters, got %d", len(proof))
	}
}

// TestConstantTimeEqualHex_Positive verifies that identical hex strings compare as equal.
func TestConstantTimeEqualHex_Positive(t *testing.T) {
	// Generate a deterministic hash to compare
	salt := []byte("test-salt")
	hash, err := HMACSHA256Hex(salt, "input")
	if err != nil {
		t.Fatalf("HMACSHA256Hex failed: %v", err)
	}

	equal, err := ConstantTimeEqualHex(hash, hash)
	if err != nil {
		t.Fatalf("ConstantTimeEqualHex failed: %v", err)
	}

	if !equal {
		t.Errorf("Identical hashes should be equal")
	}
}

// TestConstantTimeEqualHex_Negative verifies that different hex strings compare as unequal.
func TestConstantTimeEqualHex_Negative(t *testing.T) {
	salt := []byte("test-salt")

	hash1, err := HMACSHA256Hex(salt, "input1")
	if err != nil {
		t.Fatalf("HMACSHA256Hex failed: %v", err)
	}

	hash2, err := HMACSHA256Hex(salt, "input2")
	if err != nil {
		t.Fatalf("HMACSHA256Hex failed: %v", err)
	}

	equal, err := ConstantTimeEqualHex(hash1, hash2)
	if err != nil {
		t.Fatalf("ConstantTimeEqualHex failed: %v", err)
	}

	if equal {
		t.Errorf("Different hashes should not be equal")
	}
}

// TestConstantTimeEqualHex_InvalidHex verifies that invalid hex is rejected.
func TestConstantTimeEqualHex_InvalidHex(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{"invalid first arg", "not-hex", "0123456789abcdef"},
		{"invalid second arg", "0123456789abcdef", "not-hex"},
		{"both invalid", "xyz", "123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ConstantTimeEqualHex(tt.a, tt.b)
			if err != ErrInvalidHex {
				t.Errorf("Expected ErrInvalidHex, got %v", err)
			}
		})
	}
}

// TestConstantTimeEqualHex_DifferentLengths verifies behavior with different-length inputs.
func TestConstantTimeEqualHex_DifferentLengths(t *testing.T) {
	// Even if both are valid hex, different lengths should not be equal
	a := "abcd"
	b := "abcdef"

	equal, err := ConstantTimeEqualHex(a, b)
	if err != nil {
		t.Fatalf("ConstantTimeEqualHex failed: %v", err)
	}

	if equal {
		t.Errorf("Different-length hex strings should not be equal")
	}
}

// TestHMACSHA256Hex_CryptoRandomSalt demonstrates usage with crypto/rand.
// This is not a test of correctness, but shows the recommended pattern.
func TestHMACSHA256Hex_CryptoRandomSalt(t *testing.T) {
	// Generate a cryptographically secure random salt
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		t.Fatalf("crypto/rand.Read failed: %v", err)
	}

	// Use it to hash an identifier
	hash, err := HMACSHA256Hex(salt, "customer_123")
	if err != nil {
		t.Fatalf("HMACSHA256Hex failed with random salt: %v", err)
	}

	if len(hash) != 64 {
		t.Errorf("Expected 64 hex characters, got %d", len(hash))
	}
}

// TestErrorMessages_NoLeakage verifies that error messages do not leak sensitive data.
// This is a security requirement: errors must be generic.
func TestErrorMessages_NoLeakage(t *testing.T) {
	salt := []byte("secret-salt")

	// Test empty salt error
	_, err := HMACSHA256Hex(nil, "input")
	if err == nil {
		t.Fatal("Expected error for nil salt")
	}
	errMsg := err.Error()
	if strings.Contains(errMsg, "secret") || strings.Contains(errMsg, "salt") {
		// Note: "salt" in the error message is acceptable as long as it doesn't
		// contain the actual salt value. We check for the literal "secret-salt" instead.
		if strings.Contains(errMsg, "secret-salt") {
			t.Errorf("Error message contains salt value: %s", errMsg)
		}
	}

	// Test empty input error
	_, err = HMACSHA256Hex(salt, "")
	if err == nil {
		t.Fatal("Expected error for empty input")
	}
	errMsg = err.Error()
	if strings.Contains(errMsg, "secret") {
		t.Errorf("Error message leaks sensitive data: %s", errMsg)
	}

	// Errors should be generic sentinel values
	if err != ErrEmptyInput {
		t.Errorf("Expected ErrEmptyInput sentinel, got %v", err)
	}
}
