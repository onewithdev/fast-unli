package store

import (
	"os"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	dbPath := "/tmp/test_groq.db"
	os.Remove(dbPath)
	
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer store.Close()
	defer os.Remove(dbPath)
	
	// Test adding a key
	key := &Key{
		KeyValue: "gsk_test_key_123",
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
