package store

import (
	"os"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	dbPath := "/tmp/test_fast_unli.db"
	os.Remove(dbPath)
	
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer store.Close()
	defer os.Remove(dbPath)
	
	// Test adding a key with provider
	key := &Key{
		Provider: "cerebras",
		KeyValue: "csk_test_key_123",
		Status:   StatusHealthy,
	}
	
	id, err := store.AddKey(key)
	if err != nil {
		t.Fatalf("AddKey() error = %v", err)
	}
	if id == 0 {
		t.Error("AddKey() returned 0 id")
	}
	
	// Test getting all keys
	keys, err := store.GetAllKeys()
	if err != nil {
		t.Fatalf("GetAllKeys() error = %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("len(keys) = %v, want 1", len(keys))
	}
	if keys[0].Provider != "cerebras" {
		t.Errorf("Provider = %v, want cerebras", keys[0].Provider)
	}
	
	// Test updating status
	err = store.UpdateKeyStatus(id, StatusCooldown, 1, time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("UpdateKeyStatus() error = %v", err)
	}
	
	// Verify update
	keys, _ = store.GetHealthyKeys()
	if len(keys) != 0 {
		t.Errorf("Expected 0 healthy keys after status change, got %d", len(keys))
	}
}

func TestGetKeysByProvider(t *testing.T) {
	dbPath := "/tmp/test_provider.db"
	os.Remove(dbPath)
	
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer store.Close()
	defer os.Remove(dbPath)
	
	// Add keys for different providers
	store.AddKey(&Key{Provider: "cerebras", KeyValue: "csk_1", Status: StatusHealthy})
	store.AddKey(&Key{Provider: "cerebras", KeyValue: "csk_2", Status: StatusHealthy})
	store.AddKey(&Key{Provider: "groq", KeyValue: "gsk_1", Status: StatusHealthy})
	
	// Get cerebras keys
	keys, err := store.GetKeysByProvider("cerebras")
	if err != nil {
		t.Fatalf("GetKeysByProvider() error = %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("len(keys) = %v, want 2", len(keys))
	}
}
