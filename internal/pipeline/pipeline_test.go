package pipeline

import (
	"context"
	"linker-agent/internal/matchers"
	"strings"
	"testing"
	"time"
)

// Test fixtures with synthetic IDs
var (
	testSalt = []byte("test-pipeline-salt-12345")

	// Synthetic IDs
	synUser1    = "syn_user_testuser1111"
	synUser2    = "syn_user_testuser2222"
	synCust1    = "syn_cust_testcust1111"
	synCust2    = "syn_cust_testcust2222"
	synSub1     = "syn_sub_testsub111111"
	synSub2     = "syn_sub_testsub222222"
	synInv1     = "syn_inv_testinv111111"
	synInv2     = "syn_inv_testinv222222"
	synPrice1   = "syn_price_testprice1"
	synPrice2   = "syn_price_testprice2"
	synProduct1 = "syn_prod_testprod111"
	synProduct2 = "syn_prod_testprod222"

	baseTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
)

// buildTestInputs creates a minimal dataset for pipeline testing.
func buildTestInputs() Inputs {
	usages := []matchers.UsageEvent{
		{
			User:    synCust1, // Match customer1
			Feature: "export_csv",
			SKU:     synPrice1,
			At:      baseTime,
		},
		{
			User:    synCust2, // Match customer2
			Feature: "api_call",
			SKU:     synPrice2,
			At:      baseTime + 300, // 5 minutes later
		},
	}

	subs := []matchers.SubscriptionLite{
		{
			ID:         synSub1,
			Customer:   synCust1,
			PriceIDs:   []string{synPrice1},
			ProductIDs: []string{synProduct1},
			CreatedAt:  baseTime,
			Status:     "active",
		},
		{
			ID:         synSub2,
			Customer:   synCust2,
			PriceIDs:   []string{synPrice2},
			ProductIDs: []string{synProduct2},
			CreatedAt:  baseTime,
			Status:     "active",
		},
	}

	invs := []matchers.InvoiceLite{
		{
			ID:           synInv1,
			Customer:     synCust1,
			Subscription: synSub1,
			Total:        5000,
			Currency:     "usd",
			Status:       "paid",
			CreatedAt:    baseTime,
			Paid:         true,
		},
		{
			ID:           synInv2,
			Customer:     synCust2,
			Subscription: synSub2,
			Total:        10000,
			Currency:     "usd",
			Status:       "paid",
			CreatedAt:    baseTime,
			Paid:         true,
		},
	}

	return Inputs{
		Usages: usages,
		Subs:   subs,
		Invs:   invs,
	}
}

// TestRun_HappyPath tests the basic pipeline execution without an exporter.
func TestRun_HappyPath(t *testing.T) {
	ctx := context.Background()
	inputs := buildTestInputs()

	// Create ensemble
	recipe := matchers.Recipe{
		Name:    "test",
		Version: "v1",
		Weights: map[string]float64{
			"deterministic_id":   1.0,
			"temporal_proximity": 0.5,
			"sku_overlap":        0.8,
		},
		Threshold:         0.8,
		TemporalWindowSec: 3600,
		SKUOverlapMin:     0.5,
	}

	ensemble := matchers.Ensemble{
		Recipe: recipe,
		Matchers: []matchers.Matcher{
			matchers.DeterministicIDMatcher{TenantSalt: testSalt},
			matchers.TemporalMatcher{WindowSec: 3600, TenantSalt: testSalt},
			matchers.SKUOverlapMatcher{TenantSalt: testSalt},
		},
		TenantSalt: testSalt,
	}

	// Run without exporter (nil)
	result, err := Run(ctx, ensemble, nil, inputs)
	if err != nil {
		t.Fatalf("Pipeline run failed: %v", err)
	}

	// Assertions
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Stats.UsageEvents != 2 {
		t.Errorf("Expected 2 usage events, got %d", result.Stats.UsageEvents)
	}

	if result.Stats.Subscriptions != 2 {
		t.Errorf("Expected 2 subscriptions, got %d", result.Stats.Subscriptions)
	}

	if result.Stats.Invoices != 2 {
		t.Errorf("Expected 2 invoices, got %d", result.Stats.Invoices)
	}

	if result.Stats.RawEdges == 0 {
		t.Error("Expected at least some raw edges from matchers")
	}

	if result.Stats.AcceptedEdges == 0 {
		t.Error("Expected at least some accepted edges after ensemble filtering")
	}

	// Verify edges contain only synthetic IDs
	for i, edge := range result.Edges {
		if !strings.HasPrefix(edge.From, "syn_") {
			t.Errorf("Edge[%d].From is not synthetic: %s", i, edge.From)
		}
		if !strings.HasPrefix(edge.To, "syn_") {
			t.Errorf("Edge[%d].To is not synthetic: %s", i, edge.To)
		}
		if edge.Proof == "" {
			t.Errorf("Edge[%d].Proof is empty", i)
		}
	}

	t.Logf("Pipeline stats: %d raw edges -> %d accepted edges", result.Stats.RawEdges, result.Stats.AcceptedEdges)
	t.Logf("  High: %d, Medium: %d, Low: %d", result.Stats.HighConfidence, result.Stats.MediumConfidence, result.Stats.LowConfidence)
}

// TestRun_EmptyInputs tests that the pipeline handles empty inputs gracefully.
func TestRun_EmptyInputs(t *testing.T) {
	ctx := context.Background()

	recipe := matchers.Recipe{
		Name:      "test",
		Version:   "v1",
		Weights:   map[string]float64{"deterministic_id": 1.0},
		Threshold: 0.8,
	}

	ensemble := matchers.Ensemble{
		Recipe:     recipe,
		Matchers:   []matchers.Matcher{matchers.DeterministicIDMatcher{TenantSalt: testSalt}},
		TenantSalt: testSalt,
	}

	inputs := Inputs{
		Usages: []matchers.UsageEvent{},
		Subs:   []matchers.SubscriptionLite{},
		Invs:   []matchers.InvoiceLite{},
	}

	result, err := Run(ctx, ensemble, nil, inputs)
	if err != nil {
		t.Fatalf("Pipeline run failed: %v", err)
	}

	if result.Stats.AcceptedEdges != 0 {
		t.Errorf("Expected 0 accepted edges with empty inputs, got %d", result.Stats.AcceptedEdges)
	}
}

// TestValidateInputs_SyntheticIDs tests input validation for synthetic IDs.
func TestValidateInputs_SyntheticIDs(t *testing.T) {
	tests := []struct {
		name    string
		inputs  Inputs
		wantErr bool
	}{
		{
			name: "valid synthetic IDs",
			inputs: Inputs{
				Usages: []matchers.UsageEvent{
					{User: "syn_user_123", SKU: "syn_price_abc", At: 1000},
				},
				Subs: []matchers.SubscriptionLite{
					{ID: "syn_sub_xyz", Customer: "syn_cust_abc", PriceIDs: []string{"syn_price_def"}, ProductIDs: []string{"syn_prod_ghi"}},
				},
				Invs: []matchers.InvoiceLite{
					{ID: "syn_inv_123", Customer: "syn_cust_abc", Subscription: "syn_sub_xyz"},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid user ID (not synthetic)",
			inputs: Inputs{
				Usages: []matchers.UsageEvent{
					{User: "user_123", SKU: "syn_price_abc", At: 1000},
				},
				Subs:   []matchers.SubscriptionLite{},
				Invs:   []matchers.InvoiceLite{},
			},
			wantErr: true,
		},
		{
			name: "invalid subscription ID (raw Stripe ID)",
			inputs: Inputs{
				Usages: []matchers.UsageEvent{},
				Subs: []matchers.SubscriptionLite{
					{ID: "sub_1234567890abcdef", Customer: "syn_cust_abc", PriceIDs: []string{}, ProductIDs: []string{}},
				},
				Invs: []matchers.InvoiceLite{},
			},
			wantErr: true,
		},
		{
			name: "invalid invoice customer ID",
			inputs: Inputs{
				Usages: []matchers.UsageEvent{},
				Subs:   []matchers.SubscriptionLite{},
				Invs: []matchers.InvoiceLite{
					{ID: "syn_inv_123", Customer: "cus_rawcustomerid"},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid timestamp (zero)",
			inputs: Inputs{
				Usages: []matchers.UsageEvent{
					{User: "syn_user_123", SKU: "syn_price_abc", At: 0},
				},
				Subs: []matchers.SubscriptionLite{},
				Invs: []matchers.InvoiceLite{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInputs(tt.inputs)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateInputs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestRunWithoutExport tests the convenience function.
func TestRunWithoutExport(t *testing.T) {
	ctx := context.Background()
	inputs := buildTestInputs()

	recipe := matchers.Recipe{
		Name:      "test",
		Version:   "v1",
		Weights:   map[string]float64{"deterministic_id": 1.0},
		Threshold: 0.8,
	}

	ensemble := matchers.Ensemble{
		Recipe:     recipe,
		Matchers:   []matchers.Matcher{matchers.DeterministicIDMatcher{TenantSalt: testSalt}},
		TenantSalt: testSalt,
	}

	result, err := RunWithoutExport(ctx, ensemble, inputs)
	if err != nil {
		t.Fatalf("RunWithoutExport failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Should have same behavior as Run(ctx, ensemble, nil, inputs)
	if result.Stats.ExportedEdges != 0 {
		t.Errorf("Expected 0 exported edges without exporter, got %d", result.Stats.ExportedEdges)
	}
}

// TestRun_NoMatcherEdges tests that pipeline handles case where no matchers produce edges.
func TestRun_NoMatcherEdges(t *testing.T) {
	ctx := context.Background()

	// Create inputs with non-matching data
	inputs := Inputs{
		Usages: []matchers.UsageEvent{
			{User: "syn_user_nomatch", SKU: "syn_price_nomatch", At: baseTime},
		},
		Subs: []matchers.SubscriptionLite{
			{ID: "syn_sub_nomatch", Customer: "syn_cust_different", PriceIDs: []string{"syn_price_different"}, ProductIDs: []string{}},
		},
		Invs: []matchers.InvoiceLite{},
	}

	recipe := matchers.Recipe{
		Name:      "test",
		Version:   "v1",
		Weights:   map[string]float64{"deterministic_id": 1.0},
		Threshold: 0.8,
	}

	ensemble := matchers.Ensemble{
		Recipe:     recipe,
		Matchers:   []matchers.Matcher{matchers.DeterministicIDMatcher{TenantSalt: testSalt}},
		TenantSalt: testSalt,
	}

	result, err := Run(ctx, ensemble, nil, inputs)
	if err != nil {
		t.Fatalf("Pipeline run failed: %v", err)
	}

	// Should complete without error even with no matches
	if result.Stats.AcceptedEdges != 0 {
		t.Errorf("Expected 0 accepted edges with non-matching data, got %d", result.Stats.AcceptedEdges)
	}
}
