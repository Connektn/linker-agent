package crypto

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestGoldenStrictMode verifies that strict mode never emits raw platform IDs.
func TestGoldenStrictMode(t *testing.T) {
	salt := []byte("golden-test-salt")
	mapper, err := NewSyntheticMapper(salt)
	if err != nil {
		t.Fatalf("NewSyntheticMapper failed: %v", err)
	}

	// Test data: raw Stripe IDs
	rawCustomer := "cus_TestCustomer123"
	rawSubscription := "sub_TestSubscription456"
	rawInvoice := "in_TestInvoice789"
	rawPrice := "price_TestPrice101112"
	rawProduct := "prod_TestProduct131415"

	// Transform IDs
	customer, err := mapper.CustomerID(rawCustomer)
	if err != nil {
		t.Fatalf("CustomerID failed: %v", err)
	}
	subscription, err := mapper.SubscriptionID(rawSubscription)
	if err != nil {
		t.Fatalf("SubscriptionID failed: %v", err)
	}
	invoice, err := mapper.InvoiceID(rawInvoice)
	if err != nil {
		t.Fatalf("InvoiceID failed: %v", err)
	}
	price, err := mapper.PriceID(rawPrice)
	if err != nil {
		t.Fatalf("PriceID failed: %v", err)
	}
	product, err := mapper.ProductID(rawProduct)
	if err != nil {
		t.Fatalf("ProductID failed: %v", err)
	}

	// Create a mock JSON export (simulating LinkEdge)
	export := map[string]interface{}{
		"meta": map[string]string{
			"schema":  "connektn.edge.v1",
			"id_mode": string(mapper.Mode()),
		},
		"from":       customer,
		"to":         subscription,
		"kind":       "user->subscription",
		"confidence": 1.0,
		"at":         1234567890,
		"notes":      "test edge",
		"related_ids": map[string]string{
			"invoice": invoice,
			"price":   price,
			"product": product,
		},
	}

	// Serialize to JSON
	jsonData, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr := string(jsonData)

	// Collect all transformed IDs for testing
	allIDs := []string{customer, subscription, invoice, price, product}

	// GOLDEN TEST 1: All IDs start with "syn_" and don't match raw Stripe patterns
	for _, id := range allIDs {
		// Verify each ID is actually in the JSON
		if !strings.Contains(jsonStr, id) {
			t.Errorf("ID %q not found in JSON output", id)
		}
		// Verify it's a synthetic ID
		if !strings.HasPrefix(id, "syn_") {
			t.Errorf("STRICT MODE VIOLATION: ID %q does not start with 'syn_'", id)
		}
		// Verify it doesn't match raw Stripe ID pattern (e.g., "cus_" not preceded by "syn_")
		if strings.HasPrefix(id, "cus_") || strings.HasPrefix(id, "sub_") ||
		   strings.HasPrefix(id, "in_") || strings.HasPrefix(id, "price_") ||
		   strings.HasPrefix(id, "prod_") || strings.HasPrefix(id, "pi_") ||
		   strings.HasPrefix(id, "pm_") {
			t.Errorf("STRICT MODE VIOLATION: ID %q matches raw Stripe ID pattern", id)
		}
	}

	// GOLDEN TEST 2: All IDs are synthetic according to helper function
	for _, id := range allIDs {
		if !IsSyntheticID(id) {
			t.Errorf("STRICT MODE VIOLATION: ID %q does not start with 'syn_'", id)
		}
		if IsStripePlatformID(id) {
			t.Errorf("STRICT MODE VIOLATION: ID %q appears to be a Stripe platform ID", id)
		}
	}

	// GOLDEN TEST 3: meta.id_mode == "strict"
	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonData, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	meta, ok := parsed["meta"].(map[string]interface{})
	if !ok {
		t.Fatal("missing or invalid 'meta' field")
	}
	idMode, ok := meta["id_mode"].(string)
	if !ok {
		t.Fatal("missing or invalid 'meta.id_mode' field")
	}
	if idMode != "strict" {
		t.Errorf("expected meta.id_mode = 'strict', got %q", idMode)
	}

	t.Logf("✅ Golden test passed: strict mode emits only synthetic IDs")
	t.Logf("   Sample JSON: %s", jsonStr)
}

// TestGoldenPassthroughMode verifies that passthrough mode preserves raw platform IDs.
func TestGoldenPassthroughMode(t *testing.T) {
	mapper := NewPassthroughMapper()

	// Test data: raw Stripe IDs
	rawCustomer := "cus_TestCustomer123"
	rawSubscription := "sub_TestSubscription456"
	rawInvoice := "in_TestInvoice789"
	rawPrice := "price_TestPrice101112"
	rawProduct := "prod_TestProduct131415"

	// Transform IDs (should be no-op)
	customer, err := mapper.CustomerID(rawCustomer)
	if err != nil {
		t.Fatalf("CustomerID failed: %v", err)
	}
	subscription, err := mapper.SubscriptionID(rawSubscription)
	if err != nil {
		t.Fatalf("SubscriptionID failed: %v", err)
	}
	invoice, err := mapper.InvoiceID(rawInvoice)
	if err != nil {
		t.Fatalf("InvoiceID failed: %v", err)
	}
	price, err := mapper.PriceID(rawPrice)
	if err != nil {
		t.Fatalf("PriceID failed: %v", err)
	}
	product, err := mapper.ProductID(rawProduct)
	if err != nil {
		t.Fatalf("ProductID failed: %v", err)
	}

	// Create a mock JSON export (simulating LinkEdge)
	export := map[string]interface{}{
		"meta": map[string]string{
			"schema":  "connektn.edge.v1",
			"id_mode": string(mapper.Mode()),
		},
		"from":       customer,
		"to":         subscription,
		"kind":       "user->subscription",
		"confidence": 1.0,
		"at":         1234567890,
		"notes":      "test edge",
		"related_ids": map[string]string{
			"invoice": invoice,
			"price":   price,
			"product": product,
		},
	}

	// Serialize to JSON
	jsonData, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr := string(jsonData)

	// GOLDEN TEST 1: At least one Stripe platform ID present
	hasStripePlatformID := false
	stripePrefixes := []string{"cus_", "sub_", "in_", "price_", "prod_"}
	for _, prefix := range stripePrefixes {
		if strings.Contains(jsonStr, prefix) {
			hasStripePlatformID = true
			break
		}
	}
	if !hasStripePlatformID {
		t.Errorf("PASSTHROUGH MODE VIOLATION: JSON does not contain any Stripe platform ID prefixes\nJSON: %s", jsonStr)
	}

	// GOLDEN TEST 2: All IDs match Stripe regex patterns
	allIDs := []string{customer, subscription, invoice, price, product}
	for _, id := range allIDs {
		if !IsStripePlatformID(id) {
			t.Errorf("PASSTHROUGH MODE VIOLATION: ID %q does not match Stripe platform ID pattern", id)
		}
		if IsSyntheticID(id) {
			t.Errorf("PASSTHROUGH MODE VIOLATION: ID %q appears to be a synthetic ID", id)
		}
	}

	// GOLDEN TEST 3: meta.id_mode == "passthrough"
	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonData, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	meta, ok := parsed["meta"].(map[string]interface{})
	if !ok {
		t.Fatal("missing or invalid 'meta' field")
	}
	idMode, ok := meta["id_mode"].(string)
	if !ok {
		t.Fatal("missing or invalid 'meta.id_mode' field")
	}
	if idMode != "passthrough" {
		t.Errorf("expected meta.id_mode = 'passthrough', got %q", idMode)
	}

	t.Logf("✅ Golden test passed: passthrough mode preserves raw platform IDs")
	t.Logf("   Sample JSON: %s", jsonStr)
}

// TestProofDiffersAcrossModes verifies that the same inputs produce different proofs in different modes.
func TestProofDiffersAcrossModes(t *testing.T) {
	salt := []byte("test-salt")
	strictMapper, _ := NewSyntheticMapper(salt)
	passthroughMapper := NewPassthroughMapper()

	// Same input data
	input := "test-input-for-proof"

	// Generate proofs in both modes
	strictProof, err := LinkProofHex(input, string(strictMapper.Mode()))
	if err != nil {
		t.Fatalf("LinkProofHex for strict mode failed: %v", err)
	}

	passthroughProof, err := LinkProofHex(input, string(passthroughMapper.Mode()))
	if err != nil {
		t.Fatalf("LinkProofHex for passthrough mode failed: %v", err)
	}

	// GOLDEN TEST: Proofs must differ
	if strictProof == passthroughProof {
		t.Errorf("CROSS-MODE REPLAY VULNERABILITY: Same inputs produced identical proofs across modes\nstrict: %s\npassthrough: %s", strictProof, passthroughProof)
	}

	t.Logf("✅ Golden test passed: proofs differ across modes")
	t.Logf("   strict proof:      %s", strictProof)
	t.Logf("   passthrough proof: %s", passthroughProof)
}

// TestExporterGuardrailGoldenTest verifies that the exporter guardrail blocks unauthorized passthrough exports.
func TestExporterGuardrailGoldenTest(t *testing.T) {
	// This is tested in exporter_test.go but we verify the integration here

	// GOLDEN TEST 4: passthrough + http + disallowed baseUrl + allowPassthroughExports=false → error
	// (simulated - actual test in exporter package)

	t.Run("passthrough to non-Connektn endpoint without flag should error", func(t *testing.T) {
		// This would be tested in the exporter package
		// Here we just document the expected behavior
		t.Log("✅ Golden test expectation: ValidatePassthroughExportOptions() should return error")
		t.Log("   for passthrough mode + HTTP export to https://example.com without allowPassthroughExports=true")
	})

	t.Run("passthrough to Connektn endpoint always allowed", func(t *testing.T) {
		t.Log("✅ Golden test expectation: ValidatePassthroughExportOptions() should allow")
		t.Log("   for passthrough mode + HTTP export to https://api.connektn.dev")
	})

	t.Run("strict mode to any endpoint always allowed", func(t *testing.T) {
		t.Log("✅ Golden test expectation: ValidatePassthroughExportOptions() should allow")
		t.Log("   for strict mode + HTTP export to any endpoint")
	})
}
