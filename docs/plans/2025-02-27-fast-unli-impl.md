# Fast Unli Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a complete HTTP load balancer for fast inference APIs (Cerebras) with automatic failover, health tracking, and key rotation.

**Architecture:** Go HTTP server using Chi router, SQLite for persistence, in-memory round-robin key pool with state machine (healthy → cooldown → sick → dead/banned), and transparent streaming proxy to provider APIs.

**Tech Stack:** Go 1.24, Chi v5, modernc.org/sqlite, standard library for HTTP client and SSE streaming

---

## Task 1: Update Config Package for Fast Unli

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 1: Write failing test for new config**

Update `internal/config/config_test.go`:

```go
package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	// Set required env vars
	os.Setenv("FAST_UNLI_API_KEY", "test-client-key")
	os.Setenv("CEREBRAS_KEYS", "key1,key2,key3")
	
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	
	if cfg.FastUnliAPIKey != "test-client-key" {
		t.Errorf("FastUnliAPIKey = %v, want test-client-key", cfg.FastUnliAPIKey)
	}
	
	if len(cfg.CerebrasKeys) != 3 {
		t.Errorf("len(CerebrasKeys) = %v, want 3", len(cfg.CerebrasKeys))
	}
	
	// Check defaults
	if cfg.Port != "8080" {
		t.Errorf("Port = %v, want 8080", cfg.Port)
	}
	
	if cfg.MaxRetryTimeout != 3*time.Minute {
		t.Errorf("MaxRetryTimeout = %v, want 3m", cfg.MaxRetryTimeout)
	}
	
	if cfg.ProviderBaseURL != "https://api.cerebras.ai/v1" {
		t.Errorf("ProviderBaseURL = %v, want cerebras default", cfg.ProviderBaseURL)
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	os.Unsetenv("FAST_UNLI_API_KEY")
	os.Unsetenv("CEREBRAS_KEYS")
	
	_, err := Load()
	if err == nil {
		t.Error("Load() should error on missing required vars")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v`
Expected: FAIL - `FastUnliAPIKey` undefined

**Step 3: Update config implementation**

Update `internal/config/config.go`:

```go
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	FastUnliAPIKey  string
	CerebrasKeys    []string
	AdminAPIKey     string
	Port            string
	DBPath          string
	MaxRetryTimeout time.Duration
	CooldownMinutes int
	SickMinutes     int
	ProviderBaseURL string
}

func Load() (*Config, error) {
	apiKey := os.Getenv("FAST_UNLI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("FAST_UNLI_API_KEY is required")
	}
	
	keysStr := os.Getenv("CEREBRAS_KEYS")
	if keysStr == "" {
		return nil, fmt.Errorf("CEREBRAS_KEYS is required")
	}
	
	keys := strings.Split(keysStr, ",")
	for i := range keys {
		keys[i] = strings.TrimSpace(keys[i])
	}
	
	cfg := &Config{
		FastUnliAPIKey:  apiKey,
		CerebrasKeys:    keys,
		AdminAPIKey:     os.Getenv("ADMIN_API_KEY"),
		Port:            getEnvDefault("PORT", "8080"),
		DBPath:          getEnvDefault("DB_PATH", "./fast_unli.db"),
		MaxRetryTimeout: getDurationDefault("MAX_RETRY_TIMEOUT", 3*time.Minute),
		CooldownMinutes: getIntDefault("COOLDOWN_MINUTES", 10),
		SickMinutes:     getIntDefault("SICK_MINUTES", 30),
		ProviderBaseURL: getEnvDefault("PROVIDER_BASE_URL", "https://api.cerebras.ai/v1"),
	}
	
	return cfg, nil
}

func getEnvDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getIntDefault(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		var i int
		fmt.Sscanf(v, "%d", &i)
		if i > 0 {
			return i
		}
	}
	return defaultVal
}

func getDurationDefault(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return defaultVal
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat(config): update for Fast Unli - cerebras provider support"
```

---

## Task 2: Update Store Package for Provider Column

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`

**Step 1: Write failing test for provider column**

Update `internal/store/store_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/store/... -v`
Expected: FAIL - `Provider` field undefined, `GetKeysByProvider` undefined

**Step 3: Update store implementation**

Update `internal/store/store.go`:

```go
package store

import (
	"database/sql"
	"fmt"
	"time"
	
	_ "modernc.org/sqlite"
)

type KeyStatus string

const (
	StatusHealthy  KeyStatus = "healthy"
	StatusCooldown KeyStatus = "cooldown"
	StatusSick     KeyStatus = "sick"
	StatusDead     KeyStatus = "dead"
	StatusBanned   KeyStatus = "banned"
)

type Key struct {
	ID             int64
	Provider       string
	KeyValue       string
	Status         KeyStatus
	FailCount      int
	LastUsedAt     *time.Time
	LastFailedAt   *time.Time
	CooldownUntil  *time.Time
	TotalRequests  int
	TotalFailures  int
	CreatedAt      time.Time
}

type Store struct {
	db *sql.DB
}

func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	
	store := &Store{db: db}
	if err := store.createTables(); err != nil {
		return nil, fmt.Errorf("create tables: %w", err)
	}
	
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) createTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS api_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		provider TEXT NOT NULL DEFAULT 'cerebras',
		key_value TEXT NOT NULL UNIQUE,
		status TEXT NOT NULL DEFAULT 'healthy' CHECK(status IN ('healthy', 'cooldown', 'sick', 'dead', 'banned')),
		fail_count INTEGER DEFAULT 0,
		last_used_at DATETIME,
		last_failed_at DATETIME,
		cooldown_until DATETIME,
		total_requests INTEGER DEFAULT 0,
		total_failures INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_status ON api_keys(status);
	CREATE INDEX IF NOT EXISTS idx_cooldown ON api_keys(cooldown_until);
	CREATE INDEX IF NOT EXISTS idx_provider ON api_keys(provider);
	`
	
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) AddKey(key *Key) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO api_keys (provider, key_value, status) VALUES (?, ?, ?)
		 ON CONFLICT(key_value) DO UPDATE SET status = excluded.status`,
		key.Provider, key.KeyValue, key.Status,
	)
	if err != nil {
		return 0, fmt.Errorf("add key: %w", err)
	}
	return res.LastInsertId()
}

func (s *Store) GetAllKeys() ([]*Key, error) {
	rows, err := s.db.Query(`
		SELECT id, provider, key_value, status, fail_count, last_used_at, last_failed_at, 
		       cooldown_until, total_requests, total_failures, created_at
		FROM api_keys ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("get all keys: %w", err)
	}
	defer rows.Close()
	
	return s.scanKeys(rows)
}

func (s *Store) GetKeysByProvider(provider string) ([]*Key, error) {
	rows, err := s.db.Query(`
		SELECT id, provider, key_value, status, fail_count, last_used_at, last_failed_at, 
		       cooldown_until, total_requests, total_failures, created_at
		FROM api_keys WHERE provider = ? ORDER BY id
	`, provider)
	if err != nil {
		return nil, fmt.Errorf("get keys by provider: %w", err)
	}
	defer rows.Close()
	
	return s.scanKeys(rows)
}

func (s *Store) GetHealthyKeys() ([]*Key, error) {
	// Return keys that are healthy or have expired cooldown/sick status
	rows, err := s.db.Query(`
		SELECT id, provider, key_value, status, fail_count, last_used_at, last_failed_at, 
		       cooldown_until, total_requests, total_failures, created_at
		FROM api_keys 
		WHERE status = 'healthy'
		   OR (status IN ('cooldown', 'sick') AND cooldown_until <= datetime('now'))
		ORDER BY last_used_at ASC NULLS FIRST, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("get healthy keys: %w", err)
	}
	defer rows.Close()
	
	return s.scanKeys(rows)
}

func (s *Store) GetHealthyKeysByProvider(provider string) ([]*Key, error) {
	rows, err := s.db.Query(`
		SELECT id, provider, key_value, status, fail_count, last_used_at, last_failed_at, 
		       cooldown_until, total_requests, total_failures, created_at
		FROM api_keys 
		WHERE provider = ?
		  AND (status = 'healthy'
		   OR (status IN ('cooldown', 'sick') AND cooldown_until <= datetime('now')))
		ORDER BY last_used_at ASC NULLS FIRST, id ASC
	`, provider)
	if err != nil {
		return nil, fmt.Errorf("get healthy keys by provider: %w", err)
	}
	defer rows.Close()
	
	return s.scanKeys(rows)
}

func (s *Store) PromoteExpiredKeys() error {
	_, err := s.db.Exec(`
		UPDATE api_keys 
		SET status = 'healthy', fail_count = 0, cooldown_until = NULL
		WHERE status IN ('cooldown', 'sick') 
		  AND cooldown_until <= datetime('now')
	`)
	if err != nil {
		return fmt.Errorf("promote expired keys: %w", err)
	}
	return nil
}

func (s *Store) UpdateKeyStatus(id int64, status KeyStatus, failCount int, cooldownUntil time.Time) error {
	query := `UPDATE api_keys SET status = ?, fail_count = ?`
	args := []interface{}{status, failCount}
	
	if !cooldownUntil.IsZero() {
		query += `, cooldown_until = ?`
		args = append(args, cooldownUntil)
	} else {
		query += `, cooldown_until = NULL`
	}
	
	query += ` WHERE id = ?`
	args = append(args, id)
	
	_, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("update key status: %w", err)
	}
	return nil
}

func (s *Store) RecordUsage(id int64, success bool) error {
	if success {
		_, err := s.db.Exec(`
			UPDATE api_keys 
			SET total_requests = total_requests + 1, 
			    last_used_at = datetime('now'),
			    fail_count = 0
			WHERE id = ?
		`, id)
		if err != nil {
			return fmt.Errorf("record usage success: %w", err)
		}
		return nil
	}
	
	_, err := s.db.Exec(`
		UPDATE api_keys 
		SET total_failures = total_failures + 1,
		    last_failed_at = datetime('now'),
		    fail_count = fail_count + 1
		WHERE id = ?
	`, id)
	if err != nil {
		return fmt.Errorf("record usage failure: %w", err)
	}
	return nil
}

func (s *Store) DeleteKey(id int64) error {
	_, err := s.db.Exec(`DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete key: %w", err)
	}
	return nil
}

func (s *Store) scanKeys(rows *sql.Rows) ([]*Key, error) {
	var keys []*Key
	for rows.Next() {
		k := &Key{}
		var lastUsed, lastFailed, cooldown sql.NullTime
		
		err := rows.Scan(
			&k.ID, &k.Provider, &k.KeyValue, &k.Status, &k.FailCount,
			&lastUsed, &lastFailed, &cooldown,
			&k.TotalRequests, &k.TotalFailures, &k.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		
		if lastUsed.Valid {
			k.LastUsedAt = &lastUsed.Time
		}
		if lastFailed.Valid {
			k.LastFailedAt = &lastFailed.Time
		}
		if cooldown.Valid {
			k.CooldownUntil = &cooldown.Time
		}
		
		keys = append(keys, k)
	}
	return keys, rows.Err()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/store/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): add provider column, rename table to api_keys"
```

---

## Task 3: Create Key Pool Manager

**Files:**
- Create: `internal/keypool/pool.go`
- Create: `internal/keypool/pool_test.go`

**Step 1: Write failing test**

Create `internal/keypool/pool_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/keypool/... -v`
Expected: FAIL - package not found

**Step 3: Write minimal implementation**

Create `internal/keypool/pool.go`:

```go
package keypool

import (
	"errors"
	"sync"
	"time"
	
	"groq-unli/internal/store"
)

type Store interface {
	GetHealthyKeysByProvider(provider string) ([]*store.Key, error)
	UpdateKeyStatus(id int64, status store.KeyStatus, failCount int, cooldownUntil time.Time) error
	RecordUsage(id int64, success bool) error
}

type Pool struct {
	store           Store
	provider        string
	cooldownMinutes int
	sickMinutes     int
	
	mu      sync.Mutex
	keys    []*store.Key
	nextIdx int
}

func New(store Store, provider string, cooldownMinutes, sickMinutes int) *Pool {
	return &Pool{
		store:           store,
		provider:        provider,
		cooldownMinutes: cooldownMinutes,
		sickMinutes:     sickMinutes,
	}
}

func (p *Pool) RefreshKeys() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	keys, err := p.store.GetHealthyKeysByProvider(p.provider)
	if err != nil {
		return err
	}
	
	p.keys = keys
	p.nextIdx = 0
	return nil
}

func (p *Pool) GetNextKey() (*store.Key, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if len(p.keys) == 0 {
		return nil, errors.New("no healthy keys available")
	}
	
	key := p.keys[p.nextIdx]
	p.nextIdx = (p.nextIdx + 1) % len(p.keys)
	return key, nil
}

func (p *Pool) ReportSuccess(keyID int64) {
	p.store.RecordUsage(keyID, true)
}

func (p *Pool) ReportFailure(keyID int64, statusCode int) {
	// Determine new status based on error
	var newStatus store.KeyStatus
	var cooldownUntil time.Time
	
	switch statusCode {
	case 401, 403:
		// Permanent ban for auth errors
		newStatus = store.StatusBanned
	case 429:
		// Rate limit - cooldown
		newStatus = store.StatusCooldown
		cooldownUntil = time.Now().Add(time.Duration(p.cooldownMinutes) * time.Minute)
	case 500, 502, 503, 504:
		// Server errors - cooldown
		newStatus = store.StatusCooldown
		cooldownUntil = time.Now().Add(time.Duration(p.cooldownMinutes) * time.Minute)
	default:
		// Network errors - shorter cooldown
		newStatus = store.StatusCooldown
		cooldownUntil = time.Now().Add(5 * time.Minute)
	}
	
	// Get current key state to check fail count
	p.mu.Lock()
	var key *store.Key
	for _, k := range p.keys {
		if k.ID == keyID {
			key = k
			break
		}
	}
	p.mu.Unlock()
	
	if key == nil {
		p.store.UpdateKeyStatus(keyID, newStatus, 1, cooldownUntil)
		p.store.RecordUsage(keyID, false)
		return
	}
	
	newFailCount := key.FailCount + 1
	
	// State transitions based on current state and fail count
	switch key.Status {
	case store.StatusHealthy:
		if newFailCount >= 5 {
			newStatus = store.StatusSick
			cooldownUntil = time.Now().Add(time.Duration(p.sickMinutes) * time.Minute)
		}
	case store.StatusCooldown:
		if newFailCount >= 5 {
			newStatus = store.StatusSick
			cooldownUntil = time.Now().Add(time.Duration(p.sickMinutes) * time.Minute)
		}
	case store.StatusSick:
		// Second failure while sick -> dead
		newStatus = store.StatusDead
		cooldownUntil = time.Time{}
	}
	
	p.store.UpdateKeyStatus(keyID, newStatus, newFailCount, cooldownUntil)
	p.store.RecordUsage(keyID, false)
	
	// Remove from pool if no longer healthy
	if newStatus != store.StatusHealthy && newStatus != store.StatusCooldown && newStatus != store.StatusSick {
		p.removeKeyFromPool(keyID)
	}
}

func (p *Pool) removeKeyFromPool(keyID int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	var newKeys []*store.Key
	for _, k := range p.keys {
		if k.ID != keyID {
			newKeys = append(newKeys, k)
		}
	}
	p.keys = newKeys
	if p.nextIdx >= len(p.keys) {
		p.nextIdx = 0
	}
}

func (p *Pool) EnableKey(keyID int64) error {
	return p.store.UpdateKeyStatus(keyID, store.StatusHealthy, 0, time.Time{})
}

func (p *Pool) HealthyCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.keys)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/keypool/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/keypool/
git commit -m "feat(keypool): add round-robin key pool with state machine"
```

---

## Task 4: Create Provider HTTP Client

**Files:**
- Create: `internal/provider/client.go`
- Create: `internal/provider/client_test.go`

**Step 1: Write failing test**

Create `internal/provider/client_test.go`:

```go
package provider

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_DoRequest_Success(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Expected auth header, got %v", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"content":"hello"}}]}`))
	}))
	defer server.Close()
	
	client := New(server.URL, 30*time.Second)
	
	body := `{"model":"test","messages":[{"role":"user","content":"hi"}]}`
	resp, err := client.DoRequest("test-key", "/chat/completions", []byte(body))
	if err != nil {
		t.Fatalf("DoRequest() error = %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %v, want 200", resp.StatusCode)
	}
	
	respBody, _ := io.ReadAll(resp.Body)
	if string(respBody) != `{"choices":[{"message":{"content":"hello"}}]}` {
		t.Errorf("body = %v", string(respBody))
	}
}

func TestClient_DoRequest_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Header().Set("Retry-After", "60")
	}))
	defer server.Close()
	
	client := New(server.URL, 30*time.Second)
	
	body := `{"model":"test"}`
	resp, err := client.DoRequest("test-key", "/chat/completions", []byte(body))
	if err != nil {
		t.Fatalf("DoRequest() error = %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %v, want 429", resp.StatusCode)
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		statusCode int
		wantErr    ErrorType
	}{
		{200, ErrorTypeSuccess},
		{401, ErrorTypeAuth},
		{403, ErrorTypeAuth},
		{429, ErrorTypeRateLimit},
		{500, ErrorTypeServer},
		{502, ErrorTypeServer},
		{503, ErrorTypeServer},
	}
	
	for _, tt := range tests {
		t.Run(string(tt.wantErr), func(t *testing.T) {
			got := ClassifyError(tt.statusCode)
			if got != tt.wantErr {
				t.Errorf("ClassifyError(%d) = %v, want %v", tt.statusCode, got, tt.wantErr)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/... -v`
Expected: FAIL - package not found

**Step 3: Write minimal implementation**

Create `internal/provider/client.go`:

```go
package provider

import (
	"bytes"
	"fmt"
	"net/http"
	"time"
)

type ErrorType string

const (
	ErrorTypeSuccess   ErrorType = "success"
	ErrorTypeAuth      ErrorType = "auth"
	ErrorTypeRateLimit ErrorType = "rate_limit"
	ErrorTypeServer    ErrorType = "server"
	ErrorTypeNetwork   ErrorType = "network"
)

type Client struct {
	baseURL string
	client  *http.Client
}

func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) DoRequest(apiKey, path string, body []byte) (*http.Response, error) {
	url := c.baseURL + path
	
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	
	return c.client.Do(req)
}

func (c *Client) DoRequestWithMethod(apiKey, method, path string, body []byte) (*http.Response, error) {
	url := c.baseURL + path
	
	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}
	
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	
	return c.client.Do(req)
}

func ClassifyError(statusCode int) ErrorType {
	switch statusCode {
	case 200, 201:
		return ErrorTypeSuccess
	case 401, 403:
		return ErrorTypeAuth
	case 429:
		return ErrorTypeRateLimit
	case 500, 502, 503, 504:
		return ErrorTypeServer
	default:
		return ErrorTypeNetwork
	}
}

func GetRetryAfter(resp *http.Response, defaultMinutes int) time.Duration {
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter != "" {
		// Try to parse as seconds
		var seconds int
		if _, err := fmt.Sscanf(retryAfter, "%d", &seconds); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return time.Duration(defaultMinutes) * time.Minute
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/provider/
git commit -m "feat(provider): add HTTP client with error classification"
```

---

## Task 5: Create HTTP Server with Chi Router

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/proxy.go`
- Create: `internal/server/admin.go`
- Create: `internal/server/server_test.go`

**Step 1: Write failing test**

Create `internal/server/server_test.go`:

```go
package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/server/... -v`
Expected: FAIL - package not found

**Step 3: Write minimal implementation**

Create `internal/server/server.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"time"
	
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	
	"groq-unli/internal/store"
)

type KeyPool interface {
	GetNextKey() (*store.Key, error)
	ReportSuccess(keyID int64)
	ReportFailure(keyID int64, statusCode int)
	RefreshKeys() error
	HealthyCount() int
}

type ProviderClient interface {
	DoRequest(apiKey, path string, body []byte) (*http.Response, error)
	DoRequestWithMethod(apiKey, method, path string, body []byte) (*http.Response, error)
}

type Config struct {
	APIKey      string
	AdminAPIKey string
	Pool        KeyPool
	Provider    ProviderClient
}

type Server struct {
	router *chi.Mux
	cfg    *Config
}

func New(cfg *Config) *Server {
	s := &Server{cfg: cfg}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	r := chi.NewRouter()
	
	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(3 * time.Minute))
	
	// Health check (no auth required)
	r.Get("/health", s.handleHealth)
	
	// API routes (require client API key)
	r.Route("/v1", func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Post("/chat/completions", s.handleChatCompletions)
		r.Get("/models", s.handleModels)
	})
	
	// Admin routes (require admin API key)
	r.Route("/admin", func(r chi.Router) {
		r.Use(s.adminAuthMiddleware)
		r.Get("/keys", s.handleListKeys)
		r.Post("/keys", s.handleAddKey)
		r.Delete("/keys/{id}", s.handleDeleteKey)
		r.Post("/keys/{id}/enable", s.handleEnableKey)
	})
	
	s.router = r
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		expected := "Bearer " + s.cfg.APIKey
		
		if auth != expected {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Unauthorized",
			})
			return
		}
		
		next.ServeHTTP(w, r)
	})
}

func (s *Server) adminAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AdminAPIKey == "" {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Admin API not configured",
			})
			return
		}
		
		auth := r.Header.Get("Authorization")
		expected := "Bearer " + s.cfg.AdminAPIKey
		
		if auth != expected {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Unauthorized",
			})
			return
		}
		
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "ok",
		"healthy_keys": s.cfg.Pool.HealthyCount(),
	})
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
```

Create `internal/server/proxy.go`:

```go
package server

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read body")
		return
	}
	defer r.Body.Close()
	
	// Get next available key
	key, err := s.cfg.Pool.GetNextKey()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "No healthy API keys available")
		return
	}
	
	// Make request to provider
	resp, err := s.cfg.Provider.DoRequest(key.KeyValue, "/chat/completions", body)
	if err != nil {
		s.cfg.Pool.ReportFailure(key.ID, 0) // 0 = network error
		writeError(w, http.StatusBadGateway, "Provider request failed")
		return
	}
	defer resp.Body.Close()
	
	// Handle error status codes
	if resp.StatusCode != http.StatusOK {
		s.cfg.Pool.ReportFailure(key.ID, resp.StatusCode)
		
		// Try next key if available
		if s.cfg.Pool.HealthyCount() > 0 {
			s.handleChatCompletions(w, r)
			return
		}
		
		// No more keys available
		writeError(w, http.StatusServiceUnavailable, "All API keys exhausted")
		return
	}
	
	// Success - report and stream response
	s.cfg.Pool.ReportSuccess(key.ID)
	
	// Copy headers
	for k, v := range resp.Header {
		for _, val := range v {
			w.Header().Add(k, val)
		}
	}
	w.WriteHeader(resp.StatusCode)
	
	// Stream response
	io.Copy(w, resp.Body)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	// Get next available key
	key, err := s.cfg.Pool.GetNextKey()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "No healthy API keys available")
		return
	}
	
	// Make request to provider
	resp, err := s.cfg.Provider.DoRequestWithMethod(key.KeyValue, "GET", "/models", nil)
	if err != nil {
		s.cfg.Pool.ReportFailure(key.ID, 0)
		writeError(w, http.StatusBadGateway, "Provider request failed")
		return
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		s.cfg.Pool.ReportFailure(key.ID, resp.StatusCode)
		writeError(w, http.StatusBadGateway, "Provider error")
		return
	}
	
	s.cfg.Pool.ReportSuccess(key.ID)
	
	// Copy response
	for k, v := range resp.Header {
		for _, val := range v {
			w.Header().Add(k, val)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
```

Create `internal/server/admin.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	
	"github.com/go-chi/chi/v5"
	"groq-unli/internal/store"
)

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	// This would need store access - simplified version
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"keys": []map[string]interface{}{
			{"id": 1, "status": "healthy", "masked": "***"},
		},
	})
}

func (s *Server) handleAddKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Key      string `json:"key"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	
	if req.Key == "" {
		writeError(w, http.StatusBadRequest, "Key is required")
		return
	}
	
	if req.Provider == "" {
		req.Provider = "cerebras"
	}
	
	// Would add to store here
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":       1,
		"provider": req.Provider,
		"status":   "healthy",
	})
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}
	
	_ = id // Would delete from store
	
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleEnableKey(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}
	
	_ = id // Would enable in pool
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":     id,
		"status": "healthy",
	})
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "***" + key[len(key)-4:]
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/server/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/server/
git commit -m "feat(server): add HTTP server with auth, proxy, and admin routes"
```

---

## Task 6: Create Main Entry Point

**Files:**
- Create: `cmd/server/main.go`

**Step 1: Write main.go**

Create `cmd/server/main.go`:

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	
	"groq-unli/internal/config"
	"groq-unli/internal/keypool"
	"groq-unli/internal/provider"
	"groq-unli/internal/server"
	"groq-unli/internal/store"
)

func main() {
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
		Pool:        pool,
		Provider:    prov,
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
```

**Step 2: Fix missing import**

Add to `cmd/server/main.go` imports:

```go
import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	
	"groq-unli/internal/config"
	"groq-unli/internal/keypool"
	"groq-unli/internal/provider"
	"groq-unli/internal/server"
	"groq-unli/internal/store"
)
```

**Step 3: Verify build**

Run: `go build ./cmd/server/`
Expected: SUCCESS

**Step 4: Commit**

```bash
git add cmd/server/
git commit -m "feat(main): add server entry point with graceful shutdown"
```

---

## Task 7: Add Admin Store Methods and Wire Up

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/server/admin.go`

**Step 1: Add admin methods to store**

Add to `internal/store/store.go`:

```go
func (s *Store) GetKeyByID(id int64) (*Key, error) {
	row := s.db.QueryRow(`
		SELECT id, provider, key_value, status, fail_count, last_used_at, last_failed_at, 
		       cooldown_until, total_requests, total_failures, created_at
		FROM api_keys WHERE id = ?
	`, id)
	
	k := &Key{}
	var lastUsed, lastFailed, cooldown sql.NullTime
	
	err := row.Scan(
		&k.ID, &k.Provider, &k.KeyValue, &k.Status, &k.FailCount,
		&lastUsed, &lastFailed, &cooldown,
		&k.TotalRequests, &k.TotalFailures, &k.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get key by id: %w", err)
	}
	
	if lastUsed.Valid {
		k.LastUsedAt = &lastUsed.Time
	}
	if lastFailed.Valid {
		k.LastFailedAt = &lastFailed.Time
	}
	if cooldown.Valid {
		k.CooldownUntil = &cooldown.Time
	}
	
	return k, nil
}
```

**Step 2: Update server config to include store**

Modify `internal/server/server.go` Config:

```go
type Config struct {
	APIKey      string
	AdminAPIKey string
	Pool        KeyPool
	Provider    ProviderClient
	Store       StoreInterface
}

type StoreInterface interface {
	GetAllKeys() ([]*store.Key, error)
	GetKeysByProvider(provider string) ([]*store.Key, error)
	AddKey(key *store.Key) (int64, error)
	DeleteKey(id int64) error
	GetKeyByID(id int64) (*store.Key, error)
	UpdateKeyStatus(id int64, status store.KeyStatus, failCount int, cooldownUntil time.Time) error
}
```

**Step 3: Update admin handlers**

Rewrite `internal/server/admin.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	
	"github.com/go-chi/chi/v5"
	"groq-unli/internal/store"
)

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.cfg.Store.GetAllKeys()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Database error")
		return
	}
	
	var masked []map[string]interface{}
	for _, k := range keys {
		masked = append(masked, map[string]interface{}{
			"id":             k.ID,
			"provider":       k.Provider,
			"status":         k.Status,
			"masked_key":     maskKey(k.KeyValue),
			"fail_count":     k.FailCount,
			"total_requests": k.TotalRequests,
			"total_failures": k.TotalFailures,
			"created_at":     k.CreatedAt,
		})
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"keys": masked,
	})
}

func (s *Server) handleAddKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Key      string `json:"key"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	
	if req.Key == "" {
		writeError(w, http.StatusBadRequest, "Key is required")
		return
	}
	
	if req.Provider == "" {
		req.Provider = "cerebras"
	}
	
	key := &store.Key{
		Provider: req.Provider,
		KeyValue: req.Key,
		Status:   store.StatusHealthy,
	}
	
	id, err := s.cfg.Store.AddKey(key)
	if err != nil {
		writeError(w, http.StatusConflict, "Key already exists or database error")
		return
	}
	
	// Refresh pool
	s.cfg.Pool.RefreshKeys()
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":       id,
		"provider": req.Provider,
		"status":   "healthy",
	})
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}
	
	if err := s.cfg.Store.DeleteKey(id); err != nil {
		writeError(w, http.StatusInternalServerError, "Database error")
		return
	}
	
	// Refresh pool
	s.cfg.Pool.RefreshKeys()
	
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleEnableKey(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID")
		return
	}
	
	if err := s.cfg.Store.UpdateKeyStatus(id, store.StatusHealthy, 0, time.Time{}); err != nil {
		writeError(w, http.StatusInternalServerError, "Database error")
		return
	}
	
	// Refresh pool
	s.cfg.Pool.RefreshKeys()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":     id,
		"status": "healthy",
	})
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "***" + key[len(key)-4:]
}
```

**Step 4: Update main.go to pass store**

Update `cmd/server/main.go`:

```go
srv := server.New(&server.Config{
	APIKey:      cfg.FastUnliAPIKey,
	AdminAPIKey: cfg.AdminAPIKey,
	Pool:        pool,
	Provider:    prov,
	Store:       db,
})
```

**Step 5: Verify build**

Run: `go build ./cmd/server/`
Expected: SUCCESS

**Step 6: Commit**

```bash
git add internal/server/ internal/store/ cmd/server/
git commit -m "feat(admin): wire up admin routes to database"
```

---

## Task 8: Add KeyPool EnableKey Method

**Files:**
- Modify: `internal/keypool/pool.go`

**Step 1: Add EnableKey to interface and implementation**

In `internal/server/server.go`, add to KeyPool interface:

```go
type KeyPool interface {
	GetNextKey() (*store.Key, error)
	ReportSuccess(keyID int64)
	ReportFailure(keyID int64, statusCode int)
	RefreshKeys() error
	HealthyCount() int
	EnableKey(keyID int64) error
}
```

**Step 2: Verify build**

Run: `go build ./...`
Expected: SUCCESS

**Step 3: Commit**

```bash
git add internal/server/ internal/keypool/
git commit -m "feat(keypool): add EnableKey method to interface"
```

---

## Task 9: Final Integration Test

**Files:**
- Create: `test/integration_test.go`

**Step 1: Write integration test**

Create `test/integration_test.go`:

```go
package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
	
	"groq-unli/internal/config"
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
```

**Step 2: Run integration test**

Run: `go test ./test/... -v`
Expected: PASS

**Step 3: Commit**

```bash
git add test/
git commit -m "test(integration): add end-to-end integration test"
```

---

## Task 10: Update go.mod Module Name

**Files:**
- Modify: `go.mod`

**Step 1: Update module name**

Change `go.mod`:

```go
module fast-unli

go 1.24.2

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-chi/chi/v5 v5.2.5 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.22.0 // indirect
	modernc.org/gc/v3 v3.0.0-20240107210532-573471604cb6 // indirect
	modernc.org/mathutil v1.6.0 // indirect
	modernc.org/memory v1.8.0 // indirect
	modernc.org/sqlite v1.33.0 // indirect
	modernc.org/strutil v1.2.0 // indirect
	modernc.org/token v1.1.0 // indirect
)
```

**Step 2: Update all imports**

Replace all `groq-unli` imports with `fast-unli` in:
- `internal/keypool/pool.go`
- `cmd/server/main.go`
- `test/integration_test.go`
- `internal/server/server.go`
- `internal/server/admin.go`
- `internal/server/proxy.go`

**Step 3: Verify build**

Run: `go mod tidy && go build ./...`
Expected: SUCCESS

**Step 4: Commit**

```bash
git add go.mod go.sum **/*.go
git commit -m "chore: rename module from groq-unli to fast-unli"
```

---

## Task 11: Create README

**Files:**
- Create: `README.md`

**Step 1: Write README**

Create `README.md`:

```markdown
# Fast Unli

Load balancer for fast inference APIs (Cerebras, etc.) with automatic failover, health tracking, and key rotation.

## Quick Start

```bash
# Set required env vars
export FAST_UNLI_API_KEY="your-client-facing-api-key"
export CEREBRAS_KEYS="key1,key2,key3"

# Run
export PORT=8080
go run ./cmd/server/
```

## API

### Client Endpoints (requires `FAST_UNLI_API_KEY`)

- `POST /v1/chat/completions` - Proxy to provider
- `GET /v1/models` - List available models

### Admin Endpoints (requires `ADMIN_API_KEY`)

- `GET /admin/keys` - List all keys
- `POST /admin/keys` - Add a key
- `DELETE /admin/keys/:id` - Remove a key
- `POST /admin/keys/:id/enable` - Force enable a key

### Health

- `GET /health` - Service status and key stats

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `FAST_UNLI_API_KEY` | Yes | - | Client-facing API key |
| `CEREBRAS_KEYS` | Yes | - | Comma-separated provider keys |
| `ADMIN_API_KEY` | No | - | Admin API key |
| `PORT` | No | 8080 | Server port |
| `DB_PATH` | No | `./fast_unli.db` | SQLite database path |
| `PROVIDER_BASE_URL` | No | `https://api.cerebras.ai/v1` | Provider API URL |

## Key States

- **healthy** - Active in rotation
- **cooldown** - Temporary pause (10 min)
- **sick** - Longer pause (30 min)
- **dead** - Terminal, manual intervention needed
- **banned** - Invalid key (401/403)
```

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add README"
```

---

## Final Verification

Run all tests:
```bash
go test ./... -v
```

Build binary:
```bash
go build -o fast-unli ./cmd/server/
```

Expected: All tests pass, binary created successfully.
