package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"linker-agent/internal/config"
	stripec "linker-agent/internal/connectors/stripe"
	"linker-agent/internal/exporter"
	"linker-agent/internal/matchers"
	"linker-agent/internal/models"
	"linker-agent/internal/pipeline"
)

func main() {
	// Parse command-line flags
	exportBillingOnly := flag.Bool("export-billing-only", false, "Export billing data only (skip matcher pipeline)")
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	ctx := context.Background()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Println("Configuration loaded successfully")
	log.Printf("Privacy mode: %s", cfg.Privacy.Mode)
	log.Printf("Server address: %s", cfg.Server.Addr)

	// Initialize tenant salt for synthetic ID generation
	tenantSalt := []byte(cfg.Privacy.TenantSalt)

	// Branch: export billing only (legacy) vs. normal matcher pipeline mode
	if *exportBillingOnly {
		runBillingExportMode(ctx, cfg, tenantSalt)
		return
	}

	// Default mode: run the matcher pipeline
	runMatcherPipeline(ctx, cfg, tenantSalt)
}

// seedUsage generates synthetic usage events for testing the pipeline.
// Uses actual customer and price IDs from the sanitized Stripe data to ensure matches.
// In production, this would be replaced by real usage data from your product/SDK.
//
// Privacy: All IDs are synthetic. No PII.
func seedUsage(subs []models.Subscription, invs []models.Invoice) []matchers.UsageEvent {
	now := time.Now().Unix()
	var events []matchers.UsageEvent

	// Generate usage events from the first few subscriptions to ensure matches
	for i, sub := range subs {
		if i >= 5 { // Limit to 5 usage events
			break
		}

		// Get customer ID and a price/product from the subscription
		customer := string(sub.Customer)
		if len(sub.Items) == 0 {
			continue
		}

		// Create usage event with the first price
		priceID := sub.Items[0].PriceID
		productID := sub.Items[0].ProductID

		// Create 1-2 events per subscription
		events = append(events, matchers.UsageEvent{
			User:    customer,
			Feature: "export_csv",
			SKU:     priceID,
			At:      sub.CreatedAt + int64(i*60), // Shortly after subscription creation
		})

		if productID != "" {
			events = append(events, matchers.UsageEvent{
				User:    customer,
				Feature: "api_call",
				SKU:     productID,
				At:      sub.CreatedAt + int64(i*120), // A bit later
			})
		}
	}

	// If no events were generated, create fallback events
	if len(events) == 0 {
		events = []matchers.UsageEvent{
			{
				User:    "syn_cust_fallback",
				Feature: "export_csv",
				SKU:     "syn_price_fallback",
				At:      now - 600,
			},
		}
	}

	return events
}

// runMatcherPipeline executes the matcher pipeline: fetch billing data, run matchers,
// and export link edges.
func runMatcherPipeline(ctx context.Context, cfg config.Config, tenantSalt []byte) {
	log.Println("=== Running Matcher Pipeline ===")

	// Initialize Stripe connector
	if cfg.Sources.Stripe == nil {
		log.Fatal("Stripe source configuration is required")
	}

	stripeClient, err := stripec.New(
		cfg.Sources.Stripe.APIKey,
		cfg.Sources.Stripe.Account,
		cfg.Sources.Stripe.MaxRequestsPerSecond,
	)
	if err != nil {
		log.Fatalf("Failed to create Stripe client: %v", err)
	}

	log.Println("Stripe client initialized")

	// Initialize exporter with edge-specific path
	// Always use a separate file for link edges (not the billing data file)
	edgeFilePath := "reports/link_edges.jsonl"
	edgeEndpoint := cfg.Export.Endpoint + "/ingest/edges"

	if cfg.Export.Mode == "file" || cfg.Export.Mode == "both" {
		log.Printf("Link edges will be exported to file: %s", edgeFilePath)
	}
	if cfg.Export.Mode == "http" || cfg.Export.Mode == "both" {
		log.Printf("Link edges will be exported to HTTP: %s", edgeEndpoint)
	}

	exp, err := exporter.New(exporter.Options{
		Mode:          cfg.Export.Mode,
		Endpoint:      edgeEndpoint,
		FilePath:      edgeFilePath,
		TenantKey:     "test-tenant-key", // TODO: make configurable
		BatchSize:     10,
		FlushEvery:    1 * time.Second,
		MaxRetries:    3,
		QueueCapacity: 500,
	})
	if err != nil {
		log.Fatalf("Failed to create exporter: %v", err)
	}

	log.Printf("Exporter initialized (mode: %s, file: %s)", cfg.Export.Mode, edgeFilePath)

	// Start the exporter worker
	exp.Start(ctx)
	log.Println("Exporter worker started")

	// Fetch raw Stripe data
	log.Println("Fetching subscriptions from Stripe...")
	rawSubscriptions, err := stripeClient.ListRawSubscriptions(ctx, "", 100)
	if err != nil {
		log.Fatalf("Failed to list raw subscriptions: %v", err)
	}
	log.Printf("Retrieved %d subscriptions", len(rawSubscriptions))

	log.Println("Fetching invoices from Stripe...")
	rawInvoices, err := stripeClient.ListRawInvoices(ctx, "", "", 100)
	if err != nil {
		log.Fatalf("Failed to list raw invoices: %v", err)
	}
	log.Printf("Retrieved %d invoices", len(rawInvoices))

	// Sanitize and convert to models with synthetic IDs
	var modelSubscriptions []models.Subscription
	var modelInvoices []models.Invoice

	for _, sub := range rawSubscriptions {
		modelSub, err := stripec.SanitizeSubscription(tenantSalt, sub)
		if err != nil {
			log.Printf("Warning: Failed to sanitize subscription %s: %v", sub.ID, err)
			continue
		}
		modelSubscriptions = append(modelSubscriptions, modelSub)
	}

	for _, inv := range rawInvoices {
		modelInv, err := stripec.SanitizeInvoice(tenantSalt, inv)
		if err != nil {
			log.Printf("Warning: Failed to sanitize invoice %s: %v", inv.ID, err)
			continue
		}
		modelInvoices = append(modelInvoices, modelInv)
	}

	log.Printf("Sanitized %d subscriptions and %d invoices", len(modelSubscriptions), len(modelInvoices))

	// Export billing data to separate file (before running matchers)
	if cfg.Export.Mode == "file" || cfg.Export.Mode == "both" {
		log.Printf("Billing data will be exported to file: %s", cfg.Export.FilePath)
	}
	if cfg.Export.Mode == "http" || cfg.Export.Mode == "both" {
		log.Printf("Billing data will be exported to HTTP: %s", cfg.Export.Endpoint)
	}

	billingExp, err := exporter.New(exporter.Options{
		Mode:          cfg.Export.Mode,
		Endpoint:      cfg.Export.Endpoint,
		FilePath:      cfg.Export.FilePath, // Use config file path for billing data
		TenantKey:     "test-tenant-key",
		BatchSize:     10,
		FlushEvery:    1 * time.Second,
		MaxRetries:    3,
		QueueCapacity: 500,
	})
	if err != nil {
		log.Fatalf("Failed to create billing exporter: %v", err)
	}

	billingExp.Start(ctx)
	log.Println("Starting billing data export...")

	// Enqueue billing data
	for _, sub := range modelSubscriptions {
		if err := billingExp.Enqueue(ctx, sub); err != nil {
			log.Printf("Warning: Failed to enqueue subscription: %v", err)
		}
	}
	for _, inv := range modelInvoices {
		if err := billingExp.Enqueue(ctx, inv); err != nil {
			log.Printf("Warning: Failed to enqueue invoice: %v", err)
		}
	}

	// Wait a bit for billing data to flush, then shutdown
	time.Sleep(2 * time.Second)
	shutdownCtx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()
	if err := billingExp.Shutdown(shutdownCtx1); err != nil {
		log.Printf("Warning: Billing exporter shutdown had issues: %v", err)
	}
	log.Printf("Exported %d billing records to %s", len(modelSubscriptions)+len(modelInvoices), cfg.Export.FilePath)

	// Convert to Lite structures for matchers
	subsLite := stripec.ToSubscriptionLites(modelSubscriptions)
	invsLite := stripec.ToInvoiceLites(modelInvoices)

	// Generate synthetic usage events based on actual subscription data
	usages := seedUsage(modelSubscriptions, modelInvoices)
	log.Printf("Generated %d usage events", len(usages))

	// Build ensemble from config
	recipe := matchers.Recipe{
		Name:              cfg.Matchers.Recipe.Name,
		Version:           cfg.Matchers.Recipe.Version,
		Weights:           cfg.Matchers.Recipe.Weights,
		Threshold:         cfg.Matchers.Recipe.Threshold,
		TemporalWindowSec: cfg.Matchers.Recipe.TemporalWindowSec,
		SKUOverlapMin:     cfg.Matchers.Recipe.SKUOverlapMin,
	}

	ensemble := matchers.Ensemble{
		Recipe: recipe,
		Matchers: []matchers.Matcher{
			matchers.DeterministicIDMatcher{TenantSalt: tenantSalt},
			matchers.TemporalMatcher{
				WindowSec:  recipe.TemporalWindowSec,
				TenantSalt: tenantSalt,
			},
			matchers.SKUOverlapMatcher{TenantSalt: tenantSalt},
		},
		TenantSalt: tenantSalt,
	}

	log.Printf("Ensemble configured: %s/%s (threshold: %.2f)", recipe.Name, recipe.Version, recipe.Threshold)

	// Run the pipeline
	inputs := pipeline.Inputs{
		Usages: usages,
		Subs:   subsLite,
		Invs:   invsLite,
	}

	log.Println("Running matcher pipeline...")
	result, err := pipeline.Run(ctx, ensemble, exp, inputs)
	if err != nil {
		log.Fatalf("Pipeline failed: %v", err)
	}

	// Print statistics
	log.Println()
	log.Println("=== Pipeline Results ===")
	log.Printf("Usage events:    %d", result.Stats.UsageEvents)
	log.Printf("Subscriptions:   %d", result.Stats.Subscriptions)
	log.Printf("Invoices:        %d", result.Stats.Invoices)
	log.Printf("Raw edges:       %d", result.Stats.RawEdges)
	log.Printf("Accepted edges:  %d", result.Stats.AcceptedEdges)
	log.Printf("  High conf:     %d (≥0.9)", result.Stats.HighConfidence)
	log.Printf("  Medium conf:   %d (0.6-0.9)", result.Stats.MediumConfidence)
	log.Printf("  Low conf:      %d (<0.6)", result.Stats.LowConfidence)
	log.Printf("Exported edges:  %d", result.Stats.ExportedEdges)
	log.Println()
	log.Println("Per-matcher edge counts:")
	for matcher, count := range result.Stats.MatcherStats {
		log.Printf("  %s: %d", matcher, count)
	}

	// Wait for exporter to flush
	time.Sleep(2 * time.Second)

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := exp.Shutdown(shutdownCtx); err != nil {
		log.Printf("Warning: Exporter shutdown had issues: %v", err)
	}

	log.Println("Exporter shutdown complete")

	// Print completion message
	fmt.Println()
	if cfg.Export.Mode == "file" || cfg.Export.Mode == "both" {
		fmt.Printf("✅ Pipeline run complete\n")
		fmt.Printf("   • Billing data: %s\n", cfg.Export.FilePath)
		fmt.Printf("   • Link edges:   %s\n", edgeFilePath)
	} else {
		fmt.Println("✅ Pipeline run complete — data sent to HTTP endpoint")
	}
}

// runBillingExportMode executes the legacy behavior: fetch billing data and export it.
// This mode skips the matcher pipeline and only exports sanitized billing records.
func runBillingExportMode(ctx context.Context, cfg config.Config, tenantSalt []byte) {
	log.Println("=== Running Billing Export Mode (No Matchers) ===")

	// Initialize Stripe connector
	if cfg.Sources.Stripe == nil {
		log.Fatal("Stripe source configuration is required")
	}

	stripeClient, err := stripec.New(
		cfg.Sources.Stripe.APIKey,
		cfg.Sources.Stripe.Account,
		cfg.Sources.Stripe.MaxRequestsPerSecond,
	)
	if err != nil {
		log.Fatalf("Failed to create Stripe client: %v", err)
	}

	log.Println("Stripe client initialized")

	// Perform smoke check to verify connectivity
	customerCount, err := stripeClient.SmokeCheck(ctx)
	if err != nil {
		log.Fatalf("Stripe smoke check failed: %v", err)
	}
	log.Printf("Stripe smoke check passed: found %d customers", customerCount)

	// Initialize exporter using configuration
	exp, err := exporter.New(exporter.Options{
		Mode:          cfg.Export.Mode,
		Endpoint:      cfg.Export.Endpoint,
		FilePath:      cfg.Export.FilePath,
		TenantKey:     "test-tenant-key", // TODO: make configurable
		BatchSize:     10,
		FlushEvery:    1 * time.Second,
		MaxRetries:    3,
		QueueCapacity: 500, // Increased to handle larger datasets
	})
	if err != nil {
		log.Fatalf("Failed to create exporter: %v", err)
	}

	log.Printf("Exporter initialized (mode: %s)", cfg.Export.Mode)

	// Start the exporter worker
	exp.Start(ctx)
	log.Println("Exporter worker started")

	// Fetch raw Stripe data for sanitization with synthetic IDs
	log.Println("Fetching subscriptions from Stripe...")
	rawSubscriptions, err := stripeClient.ListRawSubscriptions(ctx, "", 100)
	if err != nil {
		log.Fatalf("Failed to list raw subscriptions: %v", err)
	}
	log.Printf("Retrieved %d subscriptions", len(rawSubscriptions))

	log.Println("Fetching invoices from Stripe...")
	rawInvoices, err := stripeClient.ListRawInvoices(ctx, "", "", 100)
	if err != nil {
		log.Fatalf("Failed to list raw invoices: %v", err)
	}
	log.Printf("Retrieved %d invoices", len(rawInvoices))

	// Sanitize and convert to models with synthetic IDs
	var modelSubscriptions []models.Subscription
	var modelInvoices []models.Invoice

	// Process subscriptions with synthetic ID generation
	for _, sub := range rawSubscriptions {
		modelSub, err := stripec.SanitizeSubscription(tenantSalt, sub)
		if err != nil {
			log.Printf("Warning: Failed to sanitize subscription %s: %v", sub.ID, err)
			continue
		}
		modelSubscriptions = append(modelSubscriptions, modelSub)
	}

	// Process invoices with synthetic ID generation
	for _, inv := range rawInvoices {
		modelInv, err := stripec.SanitizeInvoice(tenantSalt, inv)
		if err != nil {
			log.Printf("Warning: Failed to sanitize invoice %s: %v", inv.ID, err)
			continue
		}
		modelInvoices = append(modelInvoices, modelInv)
	}

	log.Printf("Processed %d subscriptions with synthetic IDs", len(modelSubscriptions))
	log.Printf("Processed %d invoices with synthetic IDs", len(modelInvoices))

	// Enqueue data to exporter
	log.Println("Enqueuing subscriptions to exporter...")
	for _, sub := range modelSubscriptions {
		if err := exp.Enqueue(ctx, sub); err != nil {
			log.Printf("Warning: Failed to enqueue subscription: %v", err)
		}
	}

	log.Println("Enqueuing invoices to exporter...")
	for _, inv := range modelInvoices {
		if err := exp.Enqueue(ctx, inv); err != nil {
			log.Printf("Warning: Failed to enqueue invoice: %v", err)
		}
	}

	log.Printf("Enqueued %d items total", len(modelSubscriptions)+len(modelInvoices))

	// Wait a bit for batching
	time.Sleep(2 * time.Second)

	// Graceful shutdown of exporter
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := exp.Shutdown(shutdownCtx); err != nil {
		log.Printf("Warning: Exporter shutdown had issues: %v", err)
	}

	log.Println("Exporter shutdown complete")

	// Print completion message
	fmt.Println()
	if cfg.Export.Mode == "file" || cfg.Export.Mode == "both" {
		fmt.Printf("✅ Exporter run finished — output captured in %s\n", cfg.Export.FilePath)
	} else {
		fmt.Println("✅ Exporter run finished — data sent to HTTP endpoint")
	}
}