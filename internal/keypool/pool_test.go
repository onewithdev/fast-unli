package keypool

import (
	"testing"
	"time"
	
	"groq-unli/internal/store"
)

type mockStore struct {
	keys []*store.Key
}

func (m *mockStore) GetHealthyKeysByProvider(provider string) ([]*store.Key, error) {
	var healthy []*store.Key
	now := time.Now()
	for _, k := range m.keys {
		if k.Status == store.StatusHealthy {
			healthy = append(healthy, k)
		} else if (k.Status == store.StatusCooldown || k.Status == store.StatusSick) && 
				 k.CooldownUntil != nil && k.CooldownUntil.Before(now) {
			k.Status = store.StatusHealthy
			healthy = append(healthy, k)
		}
	}
	return healthy, nil
}

func (m *mockStore) UpdateKeyStatus(id int64, status store.KeyStatus, failCount int, cooldownUntil time.Time) error {
	for _, k := range m.keys {
		if k.ID == id {
			k.Status = status
			k.FailCount = failCount
			k.CooldownUntil = &cooldownUntil
			break
		}
	}
	return nil
}

func (m *mockStore) RecordUsage(id int64, success bool) error {
	return nil
}

func TestPool_GetNextKey(t *testing.T) {
	mock := &mockStore{
		keys: []*store.Key{
			{ID: 1, KeyValue: "key1", Status: store.StatusHealthy},
			{ID: 2, KeyValue: "key2", Status: store.StatusHealthy},
			{ID: 3, KeyValue: "key3", Status: store.StatusHealthy},
		},
	}
	
	pool := New(mock, "cerebras", 10, 30)
	pool.RefreshKeys()
	
	// Test round-robin
	key1, err := pool.GetNextKey()
	if err != nil {
		t.Fatalf("GetNextKey() error = %v", err)
	}
	if key1.ID != 1 {
		t.Errorf("first key ID = %v, want 1", key1.ID)
	}
	
	key2, err := pool.GetNextKey()
	if err != nil {
		t.Fatalf("GetNextKey() error = %v", err)
	}
	if key2.ID != 2 {
		t.Errorf("second key ID = %v, want 2", key2.ID)
	}
	
	// Third key
	key3, _ := pool.GetNextKey()
	if key3.ID != 3 {
		t.Errorf("third key ID = %v, want 3", key3.ID)
	}
	
	// Should wrap around to first
	key1Again, _ := pool.GetNextKey()
	if key1Again.ID != 1 {
		t.Errorf("wrapped key ID = %v, want 1", key1Again.ID)
	}
}

func TestPool_ReportFailure(t *testing.T) {
	mock := &mockStore{
		keys: []*store.Key{
			{ID: 1, KeyValue: "key1", Status: store.StatusHealthy, FailCount: 0},
		},
	}
	
	pool := New(mock, "cerebras", 10, 30)
	pool.RefreshKeys()
	
	// Report failure
	pool.ReportFailure(1, 429)
	
	// Key should be in cooldown
	key := mock.keys[0]
	if key.Status != store.StatusCooldown {
		t.Errorf("status = %v, want cooldown", key.Status)
	}
	if key.FailCount != 1 {
		t.Errorf("fail count = %v, want 1", key.FailCount)
	}
}

func TestPool_ReportFailure_Banned(t *testing.T) {
	mock := &mockStore{
		keys: []*store.Key{
			{ID: 1, KeyValue: "key1", Status: store.StatusHealthy},
		},
	}
	
	pool := New(mock, "cerebras", 10, 30)
	pool.RefreshKeys()
	
	// Report 401 - should ban
	pool.ReportFailure(1, 401)
	
	key := mock.keys[0]
	if key.Status != store.StatusBanned {
		t.Errorf("status = %v, want banned", key.Status)
	}
}
