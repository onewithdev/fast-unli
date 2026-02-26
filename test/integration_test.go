package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"groq-unli/internal/keypool"
	"groq-unli/internal/provider"
	"groq-unli/internal/server"
	"groq-unli/internal/store"
)

func TestIntegration_EndToEnd(t *testing.T) {
	// Setup test database
	dbPath := "/tmp/test_integration.db"
	os.Remove(dbPath)

	db, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer db.Close()
	defer os.Remove(dbPath)

	// Add test key
	db.AddKey(&store.Key{
		Provider: "cerebras",
		KeyValue: "csk_test_key",
		Status:   store.StatusHealthy,
	})

	// Create pool
	pool := keypool.New(db, "cerebras", 10, 30)
	pool.RefreshKeys()

	// Create provider client (will fail requests but that's ok for this test)
	prov := provider.New("http://invalid", 5*time.Second)

	// Create server
	srv := server.New(&server.Config{
		APIKey:      "test-client-key",
		AdminAPIKey: "test-admin-key",
		Pool:        pool,
		Provider:    prov,
		Store:       db,
	})

	// Test health endpoint
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Health status = %v, want 200", rr.Code)
	}

	var health map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &health)
	if health["healthy_keys"].(float64) != 1 {
		t.Errorf("healthy_keys = %v, want 1", health["healthy_keys"])
	}

	// Test auth failure
	req = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(`{}`)))
	rr = httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Auth fail status = %v, want 401", rr.Code)
	}

	// Test admin auth failure
	req = httptest.NewRequest("GET", "/admin/keys", nil)
	rr = httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Admin auth fail status = %v, want 401", rr.Code)
	}

	// Test admin list keys
	req = httptest.NewRequest("GET", "/admin/keys", nil)
	req.Header.Set("Authorization", "Bearer test-admin-key")
	rr = httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Admin keys status = %v, want 200", rr.Code)
	}
}
