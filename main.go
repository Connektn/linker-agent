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

	"linker-agent/internal/agent"
	"linker-agent/internal/agentid"
	"linker-agent/internal/config"
	stripec "linker-agent/internal/connectors/stripe"
	"linker-agent/internal/control"
	"linker-agent/internal/exporter"
	"linker-agent/internal/ingest/stripewebhook"
	"linker-agent/internal/matchers"
	"linker-agent/internal/models"
	"linker-agent/internal/pipeline"

	"go.uber.org/zap"
)

func main() {
	// Parse command-line flags
	exportBillingOnly := flag.Bool("export-billing-only", false, "Export billing data only (skip matcher pipeline)")
	webhookMode := flag.Bool("webhook", false, "Run in webhook server mode (listen for Stripe webhooks)")
	healthCheck := flag.Bool("health-check", false, "Run health check and exit (for Docker HEALTHCHECK)")
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	// Handle health check flag (for Docker HEALTHCHECK)
	if *healthCheck {
		if err := performHealthCheck(); err != nil {
			fmt.Fprintf(os.Stderr, "Health check failed: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	ctx := context.Background()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Println("Configuration loaded successfully")
	log.Printf("Privacy mode: %s", cfg.Privacy.Mode)
	log.Printf("ID mode: %s", cfg.Privacy.IDMode)
	log.Printf("Server address: %s", cfg.Server.Addr)

	// Log passthrough mode warnings
	if cfg.Privacy.IDMode == "passthrough" {
		if cfg.Export.Mode == "file" || cfg.Export.Mode == "both" {
			log.Println("⚠️  PASSTHROUGH MODE: exporting raw platform IDs to file (no PII)")
		}
		if cfg.Export.Mode == "http" || cfg.Export.Mode == "both" {
			log.Println("⚠️  PASSTHROUGH MODE: exporting raw platform IDs via HTTP (no PII)")
		}
	}

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
		QueueCapacity: 500,               // Increased to handle larger datasets
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

	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	// Initialize agent ID
	generator := agentid.NewGenerator("") // Uses default path: /var/lib/connektn/agent-id
	agentID, err := generator.GetOrCreate()
	if err != nil {
		log.Fatalf("Failed to get agent ID: %v", err)
	}
	log.Printf("Agent ID: %s", agentID)

	// Create agent instance
	agentInstance, err := agent.New(agent.Config{
		AgentID:        agentID,
		OrganizationID: cfg.Agent.OrganizationID,
		Version:        cfg.Agent.Version,
		InitialMode:    cfg.Privacy.Mode,
		Logger:         logger,
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	log.Printf("Agent initialized: org=%s version=%s mode=%s",
		agentInstance.OrganizationID(), agentInstance.Version(), agentInstance.GetMode())

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

	// Start heartbeat sender if enabled
	var heartbeatSender *agent.HeartbeatSender
	if cfg.Heartbeat.Enabled {
		heartbeatSender, err = agent.NewHeartbeatSender(agentInstance, agent.HeartbeatSenderConfig{
			Endpoint:     cfg.Heartbeat.Endpoint,
			Interval:     cfg.Heartbeat.Interval,
			Secret:       cfg.Heartbeat.SignatureSecret,
			QueueMetrics: exp, // Exporter implements QueueMetricsProvider
			Logger:       logger,
		})
		if err != nil {
			log.Fatalf("Failed to create heartbeat sender: %v", err)
		}

		go heartbeatSender.Start(handlerCtx)
		log.Printf("Heartbeat sender started (interval: %v, endpoint: %s)", cfg.Heartbeat.Interval, cfg.Heartbeat.Endpoint)
	}

	// Start control server if enabled
	var controlServer *control.Server
	if cfg.Control.Enabled {
		controlServer = control.NewServer(control.ServerConfig{
			ListenAddr:      cfg.Control.ListenAddr,
			SignatureSecret: cfg.Control.SignatureSecret,
			MaxClockSkew:    cfg.Control.MaxClockSkew,
			NonceCacheTTL:   cfg.Control.NonceCache.TTL,
			Logger:          logger,
		})

		// Register command handlers
		handlers := control.NewHandlers(agentInstance, logger)
		controlServer.RegisterCommand(control.CommandStop, handlers.HandleStop)
		controlServer.RegisterCommand(control.CommandStart, handlers.HandleStart)
		controlServer.RegisterCommand(control.CommandRestart, handlers.HandleRestart)
		controlServer.RegisterCommand(control.CommandSwitchMode, handlers.HandleSwitchMode)
		controlServer.RegisterCommand(control.CommandUpgrade, handlers.HandleUpgrade)

		// Set shutdown function
		agentInstance.SetShutdownFunc(handlerCancel)

		go func() {
			if err := controlServer.Start(); err != nil {
				log.Printf("Control server error: %v", err)
			}
		}()

		log.Printf("Control server started on %s", cfg.Control.ListenAddr)
	}

	// Setup HTTP server
	mux := http.NewServeMux()
	mux.Handle(cfg.Sources.Stripe.Webhook.Path, handler)

	// Add liveness probe endpoint (always returns OK if server is running)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Add readiness probe endpoint (checks if worker is ready to process webhooks)
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// Check if handler and exporter are ready
		// For now, if the server is running, we're ready
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("READY"))
	})

	// Add metrics endpoint placeholder for Prometheus
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		// TODO: Implement Prometheus metrics in Story 3
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("# Prometheus metrics placeholder\n"))
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

		// Shutdown control server
		if controlServer != nil {
			if err := controlServer.Shutdown(shutdownCtx); err != nil {
				log.Printf("Control server shutdown error: %v", err)
			}
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
	log.Println()
	log.Println("=== Connektn Linker Agent Ready ===")
	log.Printf("Agent ID: %s", agentInstance.ID())
	log.Printf("Organization: %s", agentInstance.OrganizationID())
	log.Printf("Version: %s", agentInstance.Version())
	log.Printf("Mode: %s", agentInstance.GetMode())
	log.Println()
	log.Printf("Webhook server: %s", cfg.Server.Addr)
	log.Printf("  • Webhook endpoint: %s", cfg.Sources.Stripe.Webhook.Path)
	log.Printf("  • Liveness probe: /healthz")
	log.Printf("  • Readiness probe: /readyz")
	log.Printf("  • Metrics endpoint: /metrics")
	if cfg.Control.Enabled {
		log.Printf("Control server: %s", cfg.Control.ListenAddr)
		log.Printf("  • Command endpoint: /api/control/command")
	}
	if cfg.Heartbeat.Enabled {
		log.Printf("Heartbeat: %s (every %v)", cfg.Heartbeat.Endpoint, cfg.Heartbeat.Interval)
	}
	log.Println()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}

	log.Println("Webhook server stopped")
}

// performHealthCheck performs a simple health check by connecting to the local server.
// This is used by Docker HEALTHCHECK and Kubernetes liveness probes.
func performHealthCheck() error {
	// Try to connect to the local server on default port
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	// Try localhost on port 8080 (default)
	resp, err := client.Get("http://localhost:8080/healthz")
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	return nil
}
