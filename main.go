package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"linker-agent/internal/config"
	stripec "linker-agent/internal/connectors/stripe"
	"linker-agent/internal/exporter"
	"linker-agent/internal/ingest/stripewebhook"
	"linker-agent/internal/matchers"
	"linker-agent/internal/models"
	"linker-agent/internal/pipeline"
)

func main() {
	// Parse command-line flags
	exportBillingOnly := flag.Bool("export-billing-only", false, "Export billing data only (skip matcher pipeline)")
	webhookMode := flag.Bool("webhook", false, "Run in webhook server mode (listen for Stripe webhooks)")
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

	// Branch: webhook mode vs. one-off execution modes
	if *webhookMode {
		runWebhookServer(ctx, cfg, tenantSalt)
		return
	}

	if *exportBillingOnly {
		runBillingExportMode(ctx, cfg, tenantSalt)
		return
	}

	// Default mode: run the matcher pipeline once
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

	// Initialize exporter with per-stream configuration
	exp, err := exporter.New(exporter.Options{
		Config:        cfg.Export,
		TenantKey:     "test-tenant-key", // TODO: make configurable
		QueueCapacity: 500,
	})
	if err != nil {
		log.Fatalf("Failed to create exporter: %v", err)
	}

	// Log configured export destinations
	if cfg.Export.Mode == "file" || cfg.Export.Mode == "both" {
		if edgePath, ok := cfg.Export.File.Paths["edges"]; ok {
			log.Printf("Link edges will be exported to file: %s", edgePath)
		}
		if billingPath, ok := cfg.Export.File.Paths["billing"]; ok {
			log.Printf("Billing data will be exported to file: %s", billingPath)
		}
	}
	if cfg.Export.Mode == "http" || cfg.Export.Mode == "both" {
		if edgeRoute, ok := cfg.Export.HTTP.Routes["edges"]; ok {
			log.Printf("Link edges will be exported to HTTP: %s%s", cfg.Export.HTTP.BaseURL, edgeRoute)
		}
		if billingRoute, ok := cfg.Export.HTTP.Routes["billing"]; ok {
			log.Printf("Billing data will be exported to HTTP: %s%s", cfg.Export.HTTP.BaseURL, billingRoute)
		}
	}

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

	// Export billing data using the StreamBilling stream
	log.Println("Starting billing data export...")
	for _, sub := range modelSubscriptions {
		if err := exp.Enqueue(ctx, exporter.StreamBilling, sub); err != nil {
			log.Printf("Warning: Failed to enqueue subscription: %v", err)
		}
	}
	for _, inv := range modelInvoices {
		if err := exp.Enqueue(ctx, exporter.StreamBilling, inv); err != nil {
			log.Printf("Warning: Failed to enqueue invoice: %v", err)
		}
	}
	log.Printf("Enqueued %d billing records to %s stream", len(modelSubscriptions)+len(modelInvoices), exporter.StreamBilling)

	// Wait a bit for billing data to flush
	time.Sleep(2 * time.Second)

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
		if billingPath, ok := cfg.Export.File.Paths["billing"]; ok {
			fmt.Printf("   • Billing data: %s\n", billingPath)
		}
		if edgePath, ok := cfg.Export.File.Paths["edges"]; ok {
			fmt.Printf("   • Link edges:   %s\n", edgePath)
		}
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

	// Initialize exporter with per-stream configuration
	exp, err := exporter.New(exporter.Options{
		Config:        cfg.Export,
		TenantKey:     "test-tenant-key", // TODO: make configurable
		QueueCapacity: 500, // Increased to handle larger datasets
	})
	if err != nil {
		log.Fatalf("Failed to create exporter: %v", err)
	}

	log.Printf("Exporter initialized (mode: %s)", cfg.Export.Mode)

	// Log configured export destinations for billing stream
	if cfg.Export.Mode == "file" || cfg.Export.Mode == "both" {
		if billingPath, ok := cfg.Export.File.Paths["billing"]; ok {
			log.Printf("Billing data will be exported to file: %s", billingPath)
		}
	}
	if cfg.Export.Mode == "http" || cfg.Export.Mode == "both" {
		if billingRoute, ok := cfg.Export.HTTP.Routes["billing"]; ok {
			log.Printf("Billing data will be exported to HTTP: %s%s", cfg.Export.HTTP.BaseURL, billingRoute)
		}
	}

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

	// Enqueue data to exporter using StreamBilling stream
	log.Println("Enqueuing subscriptions to exporter...")
	for _, sub := range modelSubscriptions {
		if err := exp.Enqueue(ctx, exporter.StreamBilling, sub); err != nil {
			log.Printf("Warning: Failed to enqueue subscription: %v", err)
		}
	}

	log.Println("Enqueuing invoices to exporter...")
	for _, inv := range modelInvoices {
		if err := exp.Enqueue(ctx, exporter.StreamBilling, inv); err != nil {
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
		fmt.Printf("✅ Exporter run finished\n")
		if billingPath, ok := cfg.Export.File.Paths["billing"]; ok {
			fmt.Printf("   • Billing data: %s\n", billingPath)
		}
	} else {
		fmt.Println("✅ Exporter run finished — data sent to HTTP endpoint")
	}
}

// runWebhookServer starts an HTTP server with the webhook handler.
// It runs continuously until interrupted, processing webhook events in real-time.
func runWebhookServer(ctx context.Context, cfg config.Config, tenantSalt []byte) {
	log.Println("=== Starting Webhook Server Mode ===")

	// Check if webhook is enabled
	if cfg.Sources.Stripe == nil {
		log.Fatal("Stripe source configuration is required")
	}

	if !cfg.Sources.Stripe.Webhook.Enabled {
		log.Fatal("Webhook is not enabled in configuration. Set stripe.webhook.enabled: true")
	}

	// Initialize exporter
	exp, err := exporter.New(exporter.Options{
		Config:        cfg.Export,
		TenantKey:     "test-tenant-key", // TODO: make configurable
		QueueCapacity: 500,
	})
	if err != nil {
		log.Fatalf("Failed to create exporter: %v", err)
	}

	// Start the exporter worker
	exp.Start(ctx)
	log.Println("Exporter worker started")

	// Log configured export destinations
	if cfg.Export.Mode == "file" || cfg.Export.Mode == "both" {
		if edgePath, ok := cfg.Export.File.Paths["edges"]; ok {
			log.Printf("Link edges will be exported to file: %s", edgePath)
		}
	}
	if cfg.Export.Mode == "http" || cfg.Export.Mode == "both" {
		if edgeRoute, ok := cfg.Export.HTTP.Routes["edges"]; ok {
			log.Printf("Link edges will be exported to HTTP: %s%s", cfg.Export.HTTP.BaseURL, edgeRoute)
		}
	}

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

	// Create webhook worker
	worker, err := stripewebhook.NewWorker(stripewebhook.WorkerOptions{
		StripeAPIKey:  cfg.Sources.Stripe.APIKey,
		StripeAccount: cfg.Sources.Stripe.Account,
		TenantSalt:    tenantSalt,
		Ensemble:      ensemble,
		Exporter:      exp,
		RetryConfig:   cfg.Sources.Stripe.Webhook.Retry,
		Logger:        slog.Default(),
	})
	if err != nil {
		log.Fatalf("Failed to create webhook worker: %v", err)
	}

	// Create webhook handler
	handler, err := stripewebhook.NewHandler(stripewebhook.HandlerOptions{
		Config:     cfg.Sources.Stripe.Webhook,
		Worker:     worker,
		Logger:     slog.Default(),
		QueueDepth: 1000,
	})
	if err != nil {
		log.Fatalf("Failed to create webhook handler: %v", err)
	}

	// Start handler job processor
	handlerCtx, handlerCancel := context.WithCancel(ctx)
	defer handlerCancel()
	handler.Start(handlerCtx)

	// Setup HTTP server
	mux := http.NewServeMux()
	mux.Handle(cfg.Sources.Stripe.Webhook.Path, handler)

	// Add healthz endpoint
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:    cfg.Server.Addr,
		Handler: mux,
	}

	// Handle graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutdown signal received, gracefully shutting down...")

		// Stop accepting new webhooks
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}

		// Shutdown webhook handler
		if err := handler.Shutdown(shutdownCtx); err != nil {
			log.Printf("Webhook handler shutdown error: %v", err)
		}

		// Shutdown exporter
		if err := exp.Shutdown(shutdownCtx); err != nil {
			log.Printf("Exporter shutdown error: %v", err)
		}
	}()

	// Start HTTP server
	log.Printf("Webhook server listening on %s", cfg.Server.Addr)
	log.Printf("Webhook endpoint: %s", cfg.Sources.Stripe.Webhook.Path)
	log.Printf("Healthz endpoint: /healthz")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}

	log.Println("Webhook server stopped")
}