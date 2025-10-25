package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"linker-agent/internal/config"
	"linker-agent/internal/connectors/stripe"
	"linker-agent/internal/crypto"
	"linker-agent/internal/exporter"
	"linker-agent/internal/models"
)

func main() {
	ctx := context.Background()

	// Load configuration
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Println("Configuration loaded successfully")
	log.Printf("Privacy mode: %s", cfg.Privacy.Mode)
	log.Printf("Server address: %s", cfg.Server.Addr)

	// Initialize tenant salt for synthetic ID generation
	tenantSalt := []byte(cfg.Privacy.TenantSalt)

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

	// Fetch Stripe data
	log.Println("Fetching subscriptions from Stripe...")
	subscriptions, err := stripeClient.ListSubscriptions(ctx, "", 100)
	if err != nil {
		log.Fatalf("Failed to list subscriptions: %v", err)
	}
	log.Printf("Retrieved %d subscriptions", len(subscriptions))

	log.Println("Fetching invoices from Stripe...")
	invoices, err := stripeClient.ListInvoices(ctx, "", "", 100)
	if err != nil {
		log.Fatalf("Failed to list invoices: %v", err)
	}
	log.Printf("Retrieved %d invoices", len(invoices))

	// Convert to models and apply synthetic IDs
	var modelSubscriptions []models.Subscription
	var modelInvoices []models.Invoice

	// Process subscriptions
	for _, sub := range subscriptions {
		// Generate synthetic customer ID
		synCustomerID, err := crypto.HMACSHA256Hex(tenantSalt, sub.Customer)
		if err != nil {
			log.Printf("Warning: Failed to generate synthetic ID for customer %s: %v", sub.Customer, err)
			continue
		}

		// Convert items
		var items []models.SubItem
		for _, item := range sub.Items {
			items = append(items, models.SubItem{
				PriceID:   item.PriceID,
				ProductID: item.ProductID,
				Quantity:  item.Quantity,
			})
		}

		modelSub := models.Subscription{
			ID:        sub.ID,
			Customer:  models.SynStripeID(synCustomerID),
			Status:    sub.Status,
			Items:     items,
			CreatedAt: sub.CreatedAt,
		}
		modelSubscriptions = append(modelSubscriptions, modelSub)
	}

	// Process invoices
	for _, inv := range invoices {
		// Generate synthetic customer ID
		synCustomerID, err := crypto.HMACSHA256Hex(tenantSalt, inv.Customer)
		if err != nil {
			log.Printf("Warning: Failed to generate synthetic ID for customer %s: %v", inv.Customer, err)
			continue
		}

		modelInv := models.Invoice{
			ID:           inv.ID,
			Customer:     models.SynStripeID(synCustomerID),
			Subscription: inv.Subscription,
			Total:        inv.Total,
			Currency:     inv.Currency,
			Status:       inv.Status,
			CreatedAt:    inv.CreatedAt,
			Paid:         inv.Paid,
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