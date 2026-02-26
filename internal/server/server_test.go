package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"groq-unli/internal/store"
)

type mockPool struct {
	keys []*store.Key
	idx  int
}

func (m *mockPool) GetNextKey() (*store.Key, error) {
	if len(m.keys) == 0 {
		return nil, store.ErrNoHealthyKeys
	}
	key := m.keys[m.idx]
	m.idx = (m.idx + 1) % len(m.keys)
	return key, nil
}

func (m *mockPool) ReportSuccess(keyID int64) {}

func (m *mockPool) ReportFailure(keyID int64, statusCode int) {}

func (m *mockPool) RefreshKeys() error { return nil }

func (m *mockPool) HealthyCount() int { return len(m.keys) }

func TestServer_Health(t *testing.T) {
	pool := &mockPool{keys: []*store.Key{{ID: 1, KeyValue: "test"}}}
	srv := New(&Config{
		APIKey: "client-key",
		Pool:   pool,
	})

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %v, want 200", rr.Code)
	}

	if !bytes.Contains(rr.Body.Bytes(), []byte(`"healthy_keys":1`)) {
		t.Errorf("body = %v", rr.Body.String())
	}
}

func TestServer_AuthRequired(t *testing.T) {
	pool := &mockPool{keys: []*store.Key{}}
	srv := New(&Config{
		APIKey: "client-key",
		Pool:   pool,
	})

	// Request without auth header
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(`{}`)))
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %v, want 401", rr.Code)
	}
}

func TestServer_AuthSuccess(t *testing.T) {
	pool := &mockPool{keys: []*store.Key{{ID: 1, KeyValue: "provider-key"}}}
	srv := New(&Config{
		APIKey: "client-key",
		Pool:   pool,
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer client-key")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	// Should get 503 (no provider available in test) not 401
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %v, want 503", rr.Code)
	}
}
