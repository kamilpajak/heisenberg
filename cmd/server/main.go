// Package main provides the Heisenberg SaaS API server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kamilpajak/heisenberg/internal/api"
	"github.com/kamilpajak/heisenberg/internal/auth"
	"github.com/kamilpajak/heisenberg/internal/billing"
	"github.com/kamilpajak/heisenberg/internal/database"
)

func main() {
	var (
		port        = flag.String("port", getEnv("PORT", "8080"), "Server port")
		migrateOnly = flag.Bool("migrate", false, "Run migrations and exit")
	)
	flag.Parse()

	// Required environment variables
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	// Run migrations
	log.Println("Running database migrations...")
	if err := database.Migrate(dbURL); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}
	log.Println("Migrations complete")

	if *migrateOnly {
		return
	}

	// Connect to database
	ctx := context.Background()
	db, err := database.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Initialize Kinde auth verifier
	kindeDomain := os.Getenv("KINDE_DOMAIN")
	kindeAudience := os.Getenv("KINDE_AUDIENCE")
	if kindeDomain == "" {
		log.Fatal("KINDE_DOMAIN is required (e.g., https://yourapp.kinde.com)")
	}

	authVerifier, err := auth.NewVerifier(auth.Config{
		Domain:   kindeDomain,
		Audience: kindeAudience,
	})
	if err != nil {
		log.Fatalf("Failed to create auth verifier: %v", err)
	}
	defer authVerifier.Close()

	// Initialize billing client
	stripeSecretKey := os.Getenv("STRIPE_SECRET_KEY")
	stripeWebhookSecret := os.Getenv("STRIPE_WEBHOOK_SECRET")
	if stripeSecretKey == "" {
		log.Fatal("STRIPE_SECRET_KEY is required")
	}
	billingClient := billing.NewClient(billing.Config{
		SecretKey:     stripeSecretKey,
		WebhookSecret: stripeWebhookSecret,
		PriceIDs: billing.PriceIDs{
			Team:       os.Getenv("STRIPE_PRICE_TEAM"),
			Enterprise: os.Getenv("STRIPE_PRICE_ENTERPRISE"),
		},
	})

	// Create API server
	server := api.NewServer(api.Config{
		DB:            db,
		AuthVerifier:  authVerifier,
		BillingClient: billingClient,
	})
	defer server.Close()

	// Create HTTP server
	addr := fmt.Sprintf(":%s", *port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      server,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Starting server on %s", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}

	log.Println("Server stopped")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
