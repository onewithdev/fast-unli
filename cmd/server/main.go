package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"fast-unli/internal/config"
	"fast-unli/internal/keypool"
	"fast-unli/internal/provider"
	"fast-unli/internal/server"
	"fast-unli/internal/store"
)

func main() {
	// Debug: Print environment variable status
	log.Println("=== Environment Debug ===")
	if apiKey := os.Getenv("FAST_UNLI_API_KEY"); apiKey == "" {
		log.Println("FAST_UNLI_API_KEY: NOT SET")
	} else {
		log.Printf("FAST_UNLI_API_KEY: SET (length: %d)\n", len(apiKey))
	}
	if oldKey := os.Getenv("GROQ_UNLI_API_KEY"); oldKey != "" {
		log.Printf("WARNING: Found old variable GROQ_UNLI_API_KEY - please rename to FAST_UNLI_API_KEY")
	}
	if keys := os.Getenv("CEREBRAS_KEYS"); keys == "" {
		log.Println("CEREBRAS_KEYS: NOT SET")
	} else {
		log.Printf("CEREBRAS_KEYS: SET (length: %d)\n", len(keys))
	}
	log.Println("========================")

	// Auto-load .env file (like bun/node)
	_ = godotenv.Load()

	// Load config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Open database
	db, err := store.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Add bootstrap keys from env
	for _, keyValue := range cfg.CerebrasKeys {
		key := &store.Key{
			Provider: "cerebras",
			KeyValue: keyValue,
			Status:   store.StatusHealthy,
		}
		if _, err := db.AddKey(key); err != nil {
			log.Printf("Warning: failed to add bootstrap key: %v", err)
		}
	}

	// Create key pool
	pool := keypool.New(db, "cerebras", cfg.CooldownMinutes, cfg.SickMinutes)
	if err := pool.RefreshKeys(); err != nil {
		log.Fatalf("Failed to load keys: %v", err)
	}

	// Create provider client
	prov := provider.New(cfg.ProviderBaseURL, cfg.MaxRetryTimeout)

	// Create server
	srv := server.New(&server.Config{
		APIKey:      cfg.FastUnliAPIKey,
		AdminAPIKey: cfg.AdminAPIKey,
		GodModeKey:  cfg.FastUnliGodKey,
		Pool:        pool,
		Provider:    prov,
		Store:       db,
	})

	// Start background key refresh
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := pool.RefreshKeys(); err != nil {
				log.Printf("Failed to refresh keys: %v", err)
			}
		}
	}()

	// Setup graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Start server
	httpServer := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: srv,
	}

	go func() {
		log.Printf("Server starting on :%s", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-stop
	log.Println("Shutting down...")

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
