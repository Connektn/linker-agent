package crypto

import (
	"testing"
)

func TestSyntheticMapper(t *testing.T) {
	salt := []byte("test-tenant-salt-123")
	mapper, err := NewSyntheticMapper(salt)
	if err != nil {
		t.Fatalf("NewSyntheticMapper failed: %v", err)
	}

	t.Run("Mode", func(t *testing.T) {
		if mapper.Mode() != IDModeStrict {
			t.Errorf("expected mode %q, got %q", IDModeStrict, mapper.Mode())
		}
	})

	t.Run("CustomerID", func(t *testing.T) {
		raw := "cus_test123"
		synID, err := mapper.CustomerID(raw)
		if err != nil {
			t.Fatalf("CustomerID failed: %v", err)
		}

		// Verify it's a synthetic ID
		if !IsSyntheticID(synID) {
			t.Errorf("expected synthetic ID, got %q", synID)
		}

		// Verify it's not a platform ID
		if IsStripePlatformID(synID) {
			t.Errorf("synthetic ID should not be a platform ID: %q", synID)
		}

		// Verify it starts with correct prefix
		if synID[:9] != "syn_cust_" {
			t.Errorf("expected prefix 'syn_cust_', got %q", synID)
		}

		// Verify determinism
		synID2, _ := mapper.CustomerID(raw)
		if synID != synID2 {
			t.Errorf("expected deterministic output, got %q != %q", synID, synID2)
		}
	})

	t.Run("SubscriptionID", func(t *testing.T) {
		raw := "sub_test456"
		synID, err := mapper.SubscriptionID(raw)
		if err != nil {
			t.Fatalf("SubscriptionID failed: %v", err)
		}

		if !IsSyntheticID(synID) {
			t.Errorf("expected synthetic ID, got %q", synID)
		}

		if synID[:8] != "syn_sub_" {
			t.Errorf("expected prefix 'syn_sub_', got %q", synID)
		}
	})

	t.Run("InvoiceID", func(t *testing.T) {
		raw := "in_test789"
		synID, err := mapper.InvoiceID(raw)
		if err != nil {
			t.Fatalf("InvoiceID failed: %v", err)
		}

		if !IsSyntheticID(synID) {
			t.Errorf("expected synthetic ID, got %q", synID)
		}

		if synID[:8] != "syn_inv_" {
			t.Errorf("expected prefix 'syn_inv_', got %q", synID)
		}
	})

	t.Run("PriceID", func(t *testing.T) {
		raw := "price_test101112"
		synID, err := mapper.PriceID(raw)
		if err != nil {
			t.Fatalf("PriceID failed: %v", err)
		}

		if !IsSyntheticID(synID) {
			t.Errorf("expected synthetic ID, got %q", synID)
		}

		if synID[:10] != "syn_price_" {
			t.Errorf("expected prefix 'syn_price_', got %q", synID)
		}
	})

	t.Run("ProductID", func(t *testing.T) {
		raw := "prod_test131415"
		synID, err := mapper.ProductID(raw)
		if err != nil {
			t.Fatalf("ProductID failed: %v", err)
		}

		if !IsSyntheticID(synID) {
			t.Errorf("expected synthetic ID, got %q", synID)
		}

		if synID[:9] != "syn_prod_" {
			t.Errorf("expected prefix 'syn_prod_', got %q", synID)
		}
	})

	t.Run("EmptySalt", func(t *testing.T) {
		_, err := NewSyntheticMapper(nil)
		if err != ErrEmptySalt {
			t.Errorf("expected ErrEmptySalt, got %v", err)
		}

		_, err = NewSyntheticMapper([]byte{})
		if err != ErrEmptySalt {
			t.Errorf("expected ErrEmptySalt, got %v", err)
		}
	})
}

func TestPassthroughMapper(t *testing.T) {
	mapper := NewPassthroughMapper()

	t.Run("Mode", func(t *testing.T) {
		if mapper.Mode() != IDModePassthrough {
			t.Errorf("expected mode %q, got %q", IDModePassthrough, mapper.Mode())
		}
	})

	t.Run("CustomerID", func(t *testing.T) {
		raw := "cus_test123"
		result, err := mapper.CustomerID(raw)
		if err != nil {
			t.Fatalf("CustomerID failed: %v", err)
		}

		if result != raw {
			t.Errorf("expected %q, got %q", raw, result)
		}

		// Verify it's a platform ID
		if !IsStripePlatformID(result) {
			t.Errorf("expected platform ID, got %q", result)
		}

		// Verify it's not a synthetic ID
		if IsSyntheticID(result) {
			t.Errorf("passthrough ID should not be synthetic: %q", result)
		}
	})

	t.Run("SubscriptionID", func(t *testing.T) {
		raw := "sub_test456"
		result, err := mapper.SubscriptionID(raw)
		if err != nil {
			t.Fatalf("SubscriptionID failed: %v", err)
		}

		if result != raw {
			t.Errorf("expected %q, got %q", raw, result)
		}

		if !IsStripePlatformID(result) {
			t.Errorf("expected platform ID, got %q", result)
		}
	})

	t.Run("InvoiceID", func(t *testing.T) {
		raw := "in_test789"
		result, err := mapper.InvoiceID(raw)
		if err != nil {
			t.Fatalf("InvoiceID failed: %v", err)
		}

		if result != raw {
			t.Errorf("expected %q, got %q", raw, result)
		}

		if !IsStripePlatformID(result) {
			t.Errorf("expected platform ID, got %q", result)
		}
	})

	t.Run("PriceID", func(t *testing.T) {
		raw := "price_test101112"
		result, err := mapper.PriceID(raw)
		if err != nil {
			t.Fatalf("PriceID failed: %v", err)
		}

		if result != raw {
			t.Errorf("expected %q, got %q", raw, result)
		}

		if !IsStripePlatformID(result) {
			t.Errorf("expected platform ID, got %q", result)
		}
	})

	t.Run("ProductID", func(t *testing.T) {
		raw := "prod_test131415"
		result, err := mapper.ProductID(raw)
		if err != nil {
			t.Fatalf("ProductID failed: %v", err)
		}

		if result != raw {
			t.Errorf("expected %q, got %q", raw, result)
		}

		if !IsStripePlatformID(result) {
			t.Errorf("expected platform ID, got %q", result)
		}
	})

	t.Run("EmptyInput", func(t *testing.T) {
		_, err := mapper.CustomerID("")
		if err != ErrEmptyInput {
			t.Errorf("expected ErrEmptyInput, got %v", err)
		}
	})
}

func TestValidateIDMode(t *testing.T) {
	tests := []struct {
		mode      string
		expectErr bool
	}{
		{"strict", false},
		{"passthrough", false},
		{"invalid", true},
		{"", true},
		{"STRICT", true}, // case-sensitive
		{"Passthrough", true},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			err := ValidateIDMode(tt.mode)
			if tt.expectErr && err == nil {
				t.Errorf("expected error for mode %q", tt.mode)
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error for mode %q: %v", tt.mode, err)
			}
		})
	}
}

func TestIsStripePlatformID(t *testing.T) {
	tests := []struct {
		id       string
		expected bool
	}{
		{"cus_test123", true},
		{"sub_test456", true},
		{"in_test789", true},
		{"price_test101112", true},
		{"prod_test131415", true},
		{"pi_test161718", true},
		{"pm_test192021", true},
		{"syn_cust_abc123", false},
		{"syn_sub_def456", false},
		{"random_string", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			result := IsStripePlatformID(tt.id)
			if result != tt.expected {
				t.Errorf("IsStripePlatformID(%q) = %v, expected %v", tt.id, result, tt.expected)
			}
		})
	}
}

func TestIsSyntheticID(t *testing.T) {
	tests := []struct {
		id       string
		expected bool
	}{
		{"syn_cust_abc123", true},
		{"syn_sub_def456", true},
		{"syn_inv_ghi789", true},
		{"syn_price_jkl101112", true},
		{"syn_prod_mno131415", true},
		{"cus_test123", false},
		{"sub_test456", false},
		{"random_string", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			result := IsSyntheticID(tt.id)
			if result != tt.expected {
				t.Errorf("IsSyntheticID(%q) = %v, expected %v", tt.id, result, tt.expected)
			}
		})
	}
}

func TestDifferentSaltsProduceDifferentOutputs(t *testing.T) {
	salt1 := []byte("salt1")
	salt2 := []byte("salt2")

	mapper1, _ := NewSyntheticMapper(salt1)
	mapper2, _ := NewSyntheticMapper(salt2)

	raw := "cus_test123"

	id1, _ := mapper1.CustomerID(raw)
	id2, _ := mapper2.CustomerID(raw)

	if id1 == id2 {
		t.Errorf("different salts should produce different outputs, got %q == %q", id1, id2)
	}
}

func TestCrossModeDeterminism(t *testing.T) {
	// Verify that strict mode is deterministic
	salt := []byte("test-salt")
	mapper1, _ := NewSyntheticMapper(salt)
	mapper2, _ := NewSyntheticMapper(salt)

	raw := "cus_test123"

	id1, _ := mapper1.CustomerID(raw)
	id2, _ := mapper2.CustomerID(raw)

	if id1 != id2 {
		t.Errorf("same salt should produce same output, got %q != %q", id1, id2)
	}

	// Verify that passthrough mode is deterministic
	passthrough1 := NewPassthroughMapper()
	passthrough2 := NewPassthroughMapper()

	pt1, _ := passthrough1.CustomerID(raw)
	pt2, _ := passthrough2.CustomerID(raw)

	if pt1 != pt2 {
		t.Errorf("passthrough should be deterministic, got %q != %q", pt1, pt2)
	}
}
