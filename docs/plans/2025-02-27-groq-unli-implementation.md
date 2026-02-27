# Groq Unli Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a Go HTTP proxy that load-balances across multiple Groq API keys with automatic failover and health tracking.

**Architecture:** Chi router handles incoming requests, SQLite stores key states with five-tier health system (healthy/cooldown/sick/dead/banned), in-memory queue provides round-robin rotation, and a retry loop with 3-minute timeout cycles through keys on failure.

**Tech Stack:** Go 1.21+, Chi router, modernc.org/sqlite (pure Go), standard library for HTTP/streaming

---

### Task 1: Project Setup

**Files:**
- Create: `go.mod`
- Create: `.env.example`
- Create: `.gitignore`

**Step 1: Initialize Go module with dependencies**

Run:
```bash
cd /home/mors/projects/groq-unli
go mod init groq-unli
go get github.com/go-chi/chi/v5
go get github.com/go-chi/chi/v5/middleware
go get modernc.org/sqlite
go mod tidy
```

Expected: `go.mod` and `go.sum` created with chi and sqlite deps.

**Step 2: Create .env.example**

```bash
cat > .env.example << 'EOF'
GROQ_UNLI_API_KEY=your-client-facing-api-key
GROQ_KEYS=gsk_key1,gsk_key2,gsk_key3
ADMIN_API_KEY=admin-secret-for-key-management
PORT=8080
DB_PATH=./groq_unli.db
MAX_RETRY_TIMEOUT=3m
COOLDOWN_MINUTES=10
SICK_MINUTES=30
EOF
```

**Step 3: Create .gitignore**

```bash
cat > .gitignore << 'EOF'
*.db
*.db-journal
.env
.DS_Store
groq-unli
EOF
```

**Step 4: Commit**

```bash
git add go.mod go.sum .env.example .gitignore
git commit -m "chore: initialize go module with dependencies"
```

---

### Task 2: Config Package

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Step 1: Write failing test**

Create `internal/config/config_test.go`:
```go
package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	// Set required env vars
	os.Setenv("GROQ_UNLI_API_KEY", "test-key")
	os.Setenv("GROQ_KEYS", "key1,key2,key3")
	
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	
	if cfg.GroqUnliAPIKey != "test-key" {
		t.Errorf("GroqUnliAPIKey = %v, want test-key", cfg.GroqUnliAPIKey)
	}
	
	if len(cfg.GroqKeys) != 3 {
		t.Errorf("len(GroqKeys) = %v, want 3", len(cfg.GroqKeys))
	}
	
	// Check defaults
	if cfg.Port != "8080" {
		t.Errorf("Port = %v, want 8080", cfg.Port)
	}
	
	if cfg.MaxRetryTimeout != 3*time.Minute {
		t.Errorf("MaxRetryTimeout = %v, want 3m", cfg.MaxRetryTimeout)
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	os.Unsetenv("GROQ_UNLI_API_KEY")
	os.Unsetenv("GROQ_KEYS")
	
	_, err := Load()
	if err == nil {
		t.Error("Load() should error on missing required vars")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd /home/mors/projects/groq-unli
go test ./internal/config/... -v
```

Expected: FAIL "no such file or directory"

**Step 3: Write minimal implementation**

Create `internal/config/config.go`:
```go
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	GroqUnliAPIKey  string
	GroqKeys        []string
	AdminAPIKey     string
	Port            string
	DBPath          string
	MaxRetryTimeout time.Duration
	CooldownMinutes int
	SickMinutes     int
}

func Load() (*Config, error) {
	apiKey := os.Getenv("GROQ_UNLI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GROQ_UNLI_API_KEY is required")
	}
	
	keysStr := os.Getenv("GROQ_KEYS")
	if keysStr == "" {
		return nil, fmt.Errorf("GROQ_KEYS is required")
	}
	
	keys := strings.Split(keysStr, ",")
	for i := range keys {
		keys[i] = strings.TrimSpace(keys[i])
	}
	
	cfg := &Config{
		GroqUnliAPIKey:  apiKey,
		GroqKeys:        keys,
		AdminAPIKey:     os.Getenv("ADMIN_API_KEY"),
		Port:            getEnvDefault("PORT", "8080"),
		DBPath:          getEnvDefault("DB_PATH", "./groq_unli.db"),
		MaxRetryTimeout: getDurationDefault("MAX_RETRY_TIMEOUT", 3*time.Minute),
		CooldownMinutes: getIntDefault("COOLDOWN_MINUTES", 10),
		SickMinutes:     getIntDefault("SICK_MINUTES", 30),
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

```bash
go test ./internal/config/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: add config package with env loading"
```

---

### Task 3: SQLite Store (Schema & Basic CRUD)

**Files:**
- Create: `internal/store/store.go`
- Test: `internal/store/store_test.go`

**Step 1: Write failing test**

Create `internal/store/store_test.go`:
```go
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
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/store/... -v
```

Expected: FAIL "no such file or directory"

**Step 3: Write minimal implementation**

Create `internal/store/store.go`:
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
	CREATE TABLE IF NOT EXISTS groq_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
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
	CREATE INDEX IF NOT EXISTS idx_status ON groq_keys(status);
	CREATE INDEX IF NOT EXISTS idx_cooldown ON groq_keys(cooldown_until);
	`
	
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) AddKey(key *Key) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO groq_keys (key_value, status) VALUES (?, ?)
		 ON CONFLICT(key_value) DO UPDATE SET status = excluded.status`,
		key.KeyValue, key.Status,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetAllKeys() ([]*Key, error) {
	rows, err := s.db.Query(`
		SELECT id, key_value, status, fail_count, last_used_at, last_failed_at, 
		       cooldown_until, total_requests, total_failures, created_at
		FROM groq_keys ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	return s.scanKeys(rows)
}

func (s *Store) GetHealthyKeys() ([]*Key, error) {
	// Also include keys whose cooldown has expired
	rows, err := s.db.Query(`
		SELECT id, key_value, status, fail_count, last_used_at, last_failed_at, 
		       cooldown_until, total_requests, total_failures, created_at
		FROM groq_keys 
		WHERE status = 'healthy'
		   OR (status IN ('cooldown', 'sick') AND cooldown_until <= datetime('now'))
		ORDER BY last_used_at ASC NULLS FIRST, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	keys, err := s.scanKeys(rows)
	// Auto-promote expired cooldown/sick keys to healthy
	for _, k := range keys {
		if k.Status != StatusHealthy && k.CooldownUntil != nil && k.CooldownUntil.Before(time.Now()) {
			k.Status = StatusHealthy
			k.FailCount = 0
			s.UpdateKeyStatus(k.ID, StatusHealthy, 0, time.Time{})
		}
	}
	return keys, err
}

func (s *Store) UpdateKeyStatus(id int64, status KeyStatus, failCount int, cooldownUntil time.Time) error {
	query := `UPDATE groq_keys SET status = ?, fail_count = ?`
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
	return err
}

func (s *Store) RecordUsage(id int64, success bool) error {
	if success {
		_, err := s.db.Exec(`
			UPDATE groq_keys 
			SET total_requests = total_requests + 1, 
			    last_used_at = datetime('now'),
			    fail_count = 0
			WHERE id = ?
		`, id)
		return err
	}
	
	_, err := s.db.Exec(`
		UPDATE groq_keys 
		SET total_failures = total_failures + 1,
		    last_failed_at = datetime('now'),
		    fail_count = fail_count + 1
		WHERE id = ?
	`, id)
	return err
}

func (s *Store) DeleteKey(id int64) error {
	_, err := s.db.Exec(`DELETE FROM groq_keys WHERE id = ?`, id)
	return err
}

func (s *Store) scanKeys(rows *sql.Rows) ([]*Key, error) {
	var keys []*Key
	for rows.Next() {
		k := &Key{}
		var lastUsed, lastFailed, cooldown sql.NullTime
		
		err := rows.Scan(
			&k.ID, &k.KeyValue, &k.Status, &k.FailCount,
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

```bash
go test ./internal/store/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat: add sqlite store with key CRUD and status management"
```

---

### Task 4: Key Pool Manager

**Files:**
- Create: `internal/pool/pool.go`
- Test: `internal/pool/pool_test.go`

**Step 1: Write failing test**

Create `internal/pool/pool_test.go`:
```go
package pool

import (
	"context"
	"os"
	"testing"
	"time"
	
	"groq-unli/internal/store"
)

func TestPool_GetNext(t *testing.T) {
	dbPath := "/tmp/test_pool.db"
	os.Remove(dbPath)
	defer os.Remove(dbPath)
	
	s, _ := store.New(dbPath)
	defer s.Close()
	
	// Add test keys
	s.AddKey(&store.Key{KeyValue: "key1", Status: store.StatusHealthy})
	s.AddKey(&store.Key{KeyValue: "key2", Status: store.StatusHealthy})
	s.AddKey(&store.Key{KeyValue: "key3", Status: store.StatusHealthy})
	
	pool := New(s, 10, 30)
	
	// Test round-robin
	key1, _ := pool.GetNext(context.Background())
	key2, _ := pool.GetNext(context.Background())
	key3, _ := pool.GetNext(context.Background())
	
	if key1 == nil || key2 == nil || key3 == nil {
		t.Fatal("Expected 3 keys")
	}
	
	// Should cycle back
	key4, _ := pool.GetNext(context.Background())
	if key4.ID != key1.ID {
		t.Error("Expected round-robin to cycle")
	}
}

func TestPool_MarkFailure(t *testing.T) {
	dbPath := "/tmp/test_pool2.db"
	os.Remove(dbPath)
	defer os.Remove(dbPath)
	
	s, _ := store.New(dbPath)
	defer s.Close()
	
	s.AddKey(&store.Key{KeyValue: "key1", Status: store.StatusHealthy})
	
	pool := New(s, 10, 30)
	
	key, _ := pool.GetNext(context.Background())
	
	// Mark 429 failure (cooldown)
	pool.MarkFailure(key.ID, 429, "rate limited")
	
	// Key should be in cooldown
	key2, err := pool.GetNext(context.Background())
	if err == nil {
		t.Error("Expected no healthy keys available")
	}
	if key2 != nil {
		t.Error("Expected nil key when all in cooldown")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/pool/... -v
```

Expected: FAIL "no such file or directory"

**Step 3: Write minimal implementation**

Create `internal/pool/pool.go`:
```go
package pool

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
	
	"groq-unli/internal/store"
)

type Pool struct {
	store           *store.Store
	queue           []*store.Key
	mu              sync.RWMutex
	cooldownMinutes int
	sickMinutes     int
}

func New(s *store.Store, cooldownMins, sickMins int) *Pool {
	p := &Pool{
		store:           s,
		cooldownMinutes: cooldownMins,
		sickMinutes:     sickMins,
	}
	p.refreshQueue()
	return p
}

func (p *Pool) refreshQueue() {
	keys, _ := p.store.GetHealthyKeys()
	p.mu.Lock()
	p.queue = keys
	p.mu.Unlock()
}

func (p *Pool) GetNext(ctx context.Context) (*store.Key, error) {
	p.refreshQueue()
	
	p.mu.RLock()
	queue := p.queue
	p.mu.RUnlock()
	
	if len(queue) == 0 {
		return nil, fmt.Errorf("no healthy keys available")
	}
	
	key := queue[0]
	
	// Move to back of queue (round-robin)
	p.mu.Lock()
	if len(p.queue) > 0 {
		p.queue = append(p.queue[1:], p.queue[0])
	}
	p.mu.Unlock()
	
	// Update last_used
	p.store.RecordUsage(key.ID, true)
	
	return key, nil
}

func (p *Pool) MarkFailure(keyID int64, statusCode int, errMsg string) {
	key, _ := p.getKeyByID(keyID)
	if key == nil {
		return
	}
	
	p.store.RecordUsage(keyID, false)
	
	newStatus := key.Status
	failCount := key.FailCount + 1
	var cooldownUntil time.Time
	
	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		// Permanent ban
		newStatus = store.StatusBanned
		
	case statusCode == http.StatusTooManyRequests:
		// Rate limit - cooldown
		newStatus = store.StatusCooldown
		cooldownUntil = time.Now().Add(time.Duration(p.cooldownMinutes) * time.Minute)
		
	case statusCode >= 500:
		// Server error - cooldown
		newStatus = store.StatusCooldown
		cooldownUntil = time.Now().Add(time.Duration(p.cooldownMinutes) * time.Minute)
		
	default:
		// Other errors - check consecutive failures
		if failCount >= 5 {
			if key.Status == store.StatusSick {
				// Already sick, now dead
				newStatus = store.StatusDead
			} else if key.Status == store.StatusCooldown {
				// Failed rehab from cooldown
				newStatus = store.StatusSick
				cooldownUntil = time.Now().Add(time.Duration(p.sickMinutes) * time.Minute)
			} else {
				// Healthy -> Sick directly (5 consecutive failures)
				newStatus = store.StatusSick
				cooldownUntil = time.Now().Add(time.Duration(p.sickMinutes) * time.Minute)
			}
		} else {
			// Just increment fail count, stay healthy (will retry with this key later)
			newStatus = key.Status
		}
	}
	
	// If transitioning to cooldown/sick, set cooldown time
	if newStatus == store.StatusCooldown || newStatus == store.StatusSick {
		if cooldownUntil.IsZero() {
			mins := p.cooldownMinutes
			if newStatus == store.StatusSick {
				mins = p.sickMinutes
			}
			cooldownUntil = time.Now().Add(time.Duration(mins) * time.Minute)
		}
	}
	
	p.store.UpdateKeyStatus(keyID, newStatus, failCount, cooldownUntil)
	
	// Remove from in-memory queue immediately
	p.removeFromQueue(keyID)
}

func (p *Pool) MarkSuccess(keyID int64) {
	// Reset fail count on success
	p.store.UpdateKeyStatus(keyID, store.StatusHealthy, 0, time.Time{})
}

func (p *Pool) ForceEnable(keyID int64) error {
	return p.store.UpdateKeyStatus(keyID, store.StatusHealthy, 0, time.Time{})
}

func (p *Pool) getKeyByID(id int64) (*store.Key, error) {
	keys, err := p.store.GetAllKeys()
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		if k.ID == id {
			return k, nil
		}
	}
	return nil, fmt.Errorf("key not found")
}

func (p *Pool) removeFromQueue(keyID int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	var newQueue []*store.Key
	for _, k := range p.queue {
		if k.ID != keyID {
			newQueue = append(newQueue, k)
		}
	}
	p.queue = newQueue
}

func (p *Pool) Stats() map[string]int {
	keys, _ := p.store.GetAllKeys()
	stats := map[string]int{
		"total":   len(keys),
		"healthy": 0,
		"cooldown": 0,
		"sick":    0,
		"dead":    0,
		"banned":  0,
	}
	
	for _, k := range keys {
		stats[string(k.Status)]++
	}
	return stats
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/pool/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/pool/
git commit -m "feat: add key pool with round-robin and health state management"
```

---

### Task 5: Groq Client

**Files:**
- Create: `internal/groq/client.go`
- Test: `internal/groq/client_test.go` (mock server test)

**Step 1: Write failing test**

Create `internal/groq/client_test.go`:
```go
package groq

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_Do_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Expected Authorization header with test-key")
		}
		
		w.WriteHeader(200)
		w.Write([]byte(`{"choices":[{"message":{"content":"Hello"}}]}`))
	}))
	defer server.Close()
	
	client := New(server.URL, 30*time.Second)
	
	req, _ := http.NewRequest("POST", server.URL+"/v1/chat/completions", nil)
	body := []byte(`{"model":"llama-3.1-8b","messages":[{"role":"user","content":"Hi"}]}`)
	
	resp, err := client.Do(req.Context(), "test-key", body)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		t.Errorf("Status = %d, want 200", resp.StatusCode)
	}
}

func TestClient_Do_ClassifyError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantClass  ErrorClass
	}{
		{"rate limit", 429, ErrorRateLimit},
		{"auth", 401, ErrorAuth},
		{"forbidden", 403, ErrorAuth},
		{"server", 500, ErrorServer},
		{"bad gateway", 502, ErrorServer},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()
			
			client := New(server.URL, 30*time.Second)
			req, _ := http.NewRequest("POST", server.URL+"/test", nil)
			
			resp, err := client.Do(req.Context(), "key", []byte("{}"))
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			defer resp.Body.Close()
			
			class := ClassifyError(resp, nil)
			if class != tt.wantClass {
				t.Errorf("ClassifyError() = %v, want %v", class, tt.wantClass)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/groq/... -v
```

Expected: FAIL "no such file or directory"

**Step 3: Write minimal implementation**

Create `internal/groq/client.go`:
```go
package groq

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type ErrorClass int

const (
	ErrorSuccess ErrorClass = iota
	ErrorRateLimit
	ErrorAuth
	ErrorServer
	ErrorNetwork
	ErrorOther
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string, timeout time.Duration) *Client {
	if baseURL == "" {
		baseURL = "https://api.groq.com/openai"
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) Do(ctx context.Context, apiKey string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	
	return c.httpClient.Do(req)
}

func (c *Client) GetModels(ctx context.Context, apiKey string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Authorization", "Bearer "+apiKey)
	
	return c.httpClient.Do(req)
}

func ClassifyError(resp *http.Response, err error) ErrorClass {
	if err != nil {
		// Network/timeout errors
		if err == context.DeadlineExceeded || err == context.Canceled {
			return ErrorNetwork
		}
		return ErrorNetwork
	}
	
	switch resp.StatusCode {
	case http.StatusOK:
		return ErrorSuccess
	case http.StatusTooManyRequests:
		return ErrorRateLimit
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrorAuth
	default:
		if resp.StatusCode >= 500 {
			return ErrorServer
		}
		return ErrorOther
	}
}

func GetRetryAfter(resp *http.Response) time.Duration {
	// Check Retry-After header
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		var seconds int
		if _, err := fmt.Sscanf(ra, "%d", &seconds); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return 0
}

// ProxyResponse copies response from Groq to client writer
func ProxyResponse(dst http.ResponseWriter, src *http.Response) error {
	// Copy headers
	for k, vv := range src.Header {
		for _, v := range vv {
			dst.Header().Add(k, v)
		}
	}
	dst.WriteHeader(src.StatusCode)
	
	// Copy body
	_, err := io.Copy(dst, src.Body)
	return err
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/groq/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/groq/
git commit -m "feat: add groq client with error classification"
```

---

### Task 6: HTTP Server & Handlers

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/handlers.go`
- Create: `internal/server/middleware.go`

**Step 1: Write minimal implementation (handlers are integration-tested manually)**

Create `internal/server/server.go`:
```go
package server

import (
	"net/http"
	"time"
	
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	
	"groq-unli/internal/config"
	"groq-unli/internal/groq"
	"groq-unli/internal/pool"
)

type Server struct {
	router *chi.Mux
	pool   *pool.Pool
	client *groq.Client
	config *config.Config
}

func New(cfg *config.Config, p *pool.Pool) *Server {
	s := &Server{
		router: chi.NewRouter(),
		pool:   p,
		client: groq.New("", cfg.MaxRetryTimeout),
		config: cfg,
	}
	
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	// Global middleware
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.Timeout(5 * time.Minute))
	
	// Public routes (require client API key)
	s.router.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Post("/v1/chat/completions", s.handleChatCompletion)
		r.Get("/v1/models", s.handleModels)
	})
	
	// Health endpoint (no auth)
	s.router.Get("/health", s.handleHealth)
	
	// Admin routes (require admin API key)
	s.router.Group(func(r chi.Router) {
		r.Use(s.adminAuthMiddleware)
		r.Get("/admin/keys", s.handleListKeys)
		r.Post("/admin/keys", s.handleAddKey)
		r.Delete("/admin/keys/{id}", s.handleDeleteKey)
		r.Post("/admin/keys/{id}/enable", s.handleEnableKey)
	})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}
```

Create `internal/server/middleware.go`:
```go
package server

import (
	"net/http"
	"strings"
)

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, `{"error":"Missing Authorization header"}`, http.StatusUnauthorized)
			return
		}
		
		token := strings.TrimPrefix(auth, "Bearer ")
		if token != s.config.GroqUnliAPIKey {
			http.Error(w, `{"error":"Invalid API key"}`, http.StatusUnauthorized)
			return
		}
		
		next.ServeHTTP(w, r)
	})
}

func (s *Server) adminAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.config.AdminAPIKey == "" {
			http.Error(w, `{"error":"Admin API not configured"}`, http.StatusForbidden)
			return
		}
		
		auth := r.Header.Get("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")
		if token != s.config.AdminAPIKey {
			http.Error(w, `{"error":"Invalid admin API key"}`, http.StatusUnauthorized)
			return
		}
		
		next.ServeHTTP(w, r)
	})
}
```

**Step 2: Commit partial**

```bash
git add internal/server/server.go internal/server/middleware.go
git commit -m "feat: add server setup and middleware"
```

---

### Task 7: Main Chat Completion Handler

**Files:**
- Create: `internal/server/handlers.go`
- Test: Manual testing (requires real Groq keys)

**Step 1: Write implementation**

Create `internal/server/handlers.go`:
```go
package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"
	
	"github.com/go-chi/chi/v5"
	
	"groq-unli/internal/groq"
	"groq-unli/internal/store"
)

func (s *Server) handleChatCompletion(w http.ResponseWriter, r *http.Request) {
	// Read body for retries
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"Failed to read request body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	
	// Check if streaming
	isStreaming := false
	var reqBody map[string]interface{}
	if err := json.Unmarshal(body, &reqBody); err == nil {
		if stream, ok := reqBody["stream"].(bool); ok && stream {
			isStreaming = true
		}
	}
	
	// Setup timeout context
	ctx, cancel := context.WithTimeout(r.Context(), s.config.MaxRetryTimeout)
	defer cancel()
	
	// Try keys until success or timeout
	var lastErr error
	startTime := time.Now()
	
	for {
		// Check timeout
		if time.Since(startTime) >= s.config.MaxRetryTimeout {
			http.Error(w, `{"error":"All API keys exhausted, please try again later","type":"service_unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		
		// Get next healthy key
		key, err := s.pool.GetNext(ctx)
		if err != nil {
			// No healthy keys, wait and retry
			select {
			case <-ctx.Done():
				http.Error(w, `{"error":"Request timeout","type":"timeout"}`, http.StatusServiceUnavailable)
				return
			case <-time.After(2 * time.Second):
				continue
			}
		}
		
		// Make request to Groq
		resp, err := s.client.Do(ctx, key.KeyValue, body)
		if err != nil {
			lastErr = err
			s.pool.MarkFailure(key.ID, 0, err.Error())
			continue
		}
		
		// Classify response
		errClass := groq.ClassifyError(resp, nil)
		
		switch errClass {
		case groq.ErrorSuccess:
			// Success! Reset fail count and stream response
			s.pool.MarkSuccess(key.ID)
			
			// Copy headers
			for k, vv := range resp.Header {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(resp.StatusCode)
			
			// Stream or copy body
			if isStreaming {
				// For SSE streaming, flush after each chunk
				flusher, ok := w.(http.Flusher)
				if ok {
					buf := make([]byte, 4096)
					for {
						n, err := resp.Body.Read(buf)
						if n > 0 {
							w.Write(buf[:n])
							flusher.Flush()
						}
						if err == io.EOF {
							break
						}
						if err != nil {
							break
						}
					}
				} else {
					io.Copy(w, resp.Body)
				}
			} else {
				io.Copy(w, resp.Body)
			}
			resp.Body.Close()
			return
			
		case groq.ErrorRateLimit:
			// Use retry-after header if available
			retryAfter := groq.GetRetryAfter(resp)
			if retryAfter > 0 {
				s.pool.MarkFailure(key.ID, resp.StatusCode, "rate limited")
			} else {
				s.pool.MarkFailure(key.ID, resp.StatusCode, "rate limited")
			}
			resp.Body.Close()
			continue
			
		case groq.ErrorAuth:
			// Permanent ban
			s.pool.MarkFailure(key.ID, resp.StatusCode, "auth error")
			resp.Body.Close()
			continue
			
		default:
			// Server error or other
			s.pool.MarkFailure(key.ID, resp.StatusCode, "server/error")
			resp.Body.Close()
			continue
		}
	}
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	
	// Get any healthy key
	key, err := s.pool.GetNext(ctx)
	if err != nil {
		http.Error(w, `{"error":"No healthy keys available"}`, http.StatusServiceUnavailable)
		return
	}
	
	resp, err := s.client.GetModels(ctx, key.KeyValue)
	if err != nil {
		http.Error(w, `{"error":"Failed to fetch models"}`, http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	stats := s.pool.Stats()
	
	response := map[string]interface{}{
		"status": "healthy",
		"keys":   stats,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	// Get from store directly
	keys, err := s.pool.GetAllKeys() // Need to add this method
	if err != nil {
		http.Error(w, `{"error":"Failed to list keys"}`, http.StatusInternalServerError)
		return
	}
	
	var masked []map[string]interface{}
	for _, k := range keys {
		masked = append(masked, map[string]interface{}{
			"id":             k.ID,
			"key":            maskKey(k.KeyValue),
			"status":         k.Status,
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
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid JSON"}`, http.StatusBadRequest)
		return
	}
	
	if req.Key == "" {
		http.Error(w, `{"error":"Key is required"}`, http.StatusBadRequest)
		return
	}
	
	key := &store.Key{
		KeyValue: req.Key,
		Status:   store.StatusHealthy,
	}
	
	id, err := s.pool.AddKey(key) // Need to add this method
	if err != nil {
		http.Error(w, `{"error":"Failed to add key"}`, http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      id,
		"message": "Key added successfully",
	})
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"Invalid key ID"}`, http.StatusBadRequest)
		return
	}
	
	if err := s.pool.DeleteKey(id); err != nil {
		http.Error(w, `{"error":"Failed to delete key"}`, http.StatusInternalServerError)
		return
	}
	
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleEnableKey(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"Invalid key ID"}`, http.StatusBadRequest)
		return
	}
	
	if err := s.pool.ForceEnable(id); err != nil {
		http.Error(w, `{"error":"Failed to enable key"}`, http.StatusInternalServerError)
		return
	}
	
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Key enabled successfully",
	})
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
```

**Step 2: Add missing methods to pool**

Modify `internal/pool/pool.go` to add these methods at the end:

```go
// Add these methods to pool.go after the existing methods

func (p *Pool) AddKey(key *store.Key) (int64, error) {
	return p.store.AddKey(key)
}

func (p *Pool) DeleteKey(id int64) error {
	p.removeFromQueue(id)
	return p.store.DeleteKey(id)
}

func (p *Pool) GetAllKeys() ([]*store.Key, error) {
	return p.store.GetAllKeys()
}
```

**Step 3: Commit**

```bash
git add internal/server/handlers.go internal/pool/pool.go
git commit -m "feat: add all HTTP handlers and admin endpoints"
```

---

### Task 8: Main Entry Point

**Files:**
- Create: `cmd/server/main.go`
- Create: `fly.toml`
- Create: `Dockerfile`

**Step 1: Create main.go**

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	
	"groq-unli/internal/config"
	"groq-unli/internal/pool"
	"groq-unli/internal/server"
	"groq-unli/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	
	// Initialize store
	s, err := store.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}
	defer s.Close()
	
	// Bootstrap keys from env if DB is empty
	keys, _ := s.GetAllKeys()
	if len(keys) == 0 && len(cfg.GroqKeys) > 0 {
		log.Println("Bootstrapping keys from environment...")
		for _, key := range cfg.GroqKeys {
			_, err := s.AddKey(&store.Key{
				KeyValue: key,
				Status:   store.StatusHealthy,
			})
			if err != nil {
				log.Printf("Failed to add key: %v", err)
			}
		}
	}
	
	// Initialize pool
	p := pool.New(s, cfg.CooldownMinutes, cfg.SickMinutes)
	
	// Create server
	srv := server.New(cfg, p)
	
	port := cfg.Port
	if port == "" {
		port = "8080"
	}
	
	log.Printf("Server starting on port %s", port)
	log.Printf("Health check: http://localhost:%s/health", port)
	
	if err := http.ListenAndServe(":"+port, srv); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
```

**Step 2: Create Dockerfile**

```dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o groq-unli ./cmd/server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/groq-unli .

ENV PORT=8080
ENV DB_PATH=/data/groq_unli.db

EXPOSE 8080

CMD ["./groq-unli"]
```

**Step 3: Create fly.toml**

```toml
app = "groq-unli"
primary_region = "iad"

[build]

[env]
  PORT = "8080"
  DB_PATH = "/data/groq_unli.db"

[[mounts]]
  source = "groq_unli_data"
  destination = "/data"

[http_service]
  internal_port = 8080
  force_https = true
  auto_stop_machines = false
  auto_start_machines = true
  min_machines_running = 1
  processes = ["app"]
```

**Step 4: Build and test locally**

```bash
# Create test env
cat > .env << 'EOF'
GROQ_UNLI_API_KEY=test-client-key
GROQ_KEYS=gsk_fake_key_1,gsk_fake_key_2
ADMIN_API_KEY=test-admin-key
PORT=8080
EOF

# Build
go build -o groq-unli ./cmd/server

# Run (will fail on actual requests without real keys, but starts)
timeout 5 ./groq-unli &
sleep 2
curl http://localhost:8080/health
kill %1
```

Expected: JSON response with `{"status":"healthy","keys":{"total":2,...}}`

**Step 5: Commit**

```bash
git add cmd/server/main.go Dockerfile fly.toml
git commit -m "feat: add main entry point and deployment config"
```

---

### Task 9: Testing & Final Verification

**Step 1: Run all unit tests**

```bash
go test ./... -v
```

Expected: All tests PASS

**Step 2: Manual integration test (with mock)**

Create `scripts/test.sh`:
```bash
#!/bin/bash
set -e

echo "Starting server..."
export GROQ_UNLI_API_KEY=test-client-key
export GROQ_KEYS=gsk_fake1,gsk_fake2
export ADMIN_API_KEY=test-admin
export PORT=8081

# Build and start
go build -o /tmp/groq-unli-test ./cmd/server
/tmp/groq-unli-test &
PID=$!
sleep 2

cleanup() {
    kill $PID 2>/dev/null || true
    rm -f /tmp/test*.db /tmp/groq-unli-test
}
trap cleanup EXIT

echo "=== Test 1: Health endpoint ==="
curl -s http://localhost:8081/health | jq .

echo "=== Test 2: Unauthorized (no key) ==="
curl -s -w "\nHTTP %{http_code}\n" http://localhost:8081/v1/chat/completions -X POST

echo "=== Test 3: Unauthorized (wrong key) ==="
curl -s -w "\nHTTP %{http_code}\n" http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer wrong-key" -X POST

echo "=== Test 4: Admin - List keys ==="
curl -s http://localhost:8081/admin/keys \
  -H "Authorization: Bearer test-admin" | jq .

echo "=== All tests passed ==="
```

```bash
chmod +x scripts/test.sh
./scripts/test.sh
```

**Step 3: Final commit**

```bash
git add scripts/
git commit -m "chore: add integration test script"
```

---

## Summary

**What was built:**
1. ✅ Config package with env loading
2. ✅ SQLite store with key persistence and health states
3. ✅ Key pool with round-robin rotation and auto state transitions
4. ✅ Groq client with error classification
5. ✅ HTTP server with Chi router, auth middleware
6. ✅ Main proxy endpoint with retry logic and streaming
7. ✅ Admin API for key management
8. ✅ Health endpoint with stats
9. ✅ Fly.io deployment config

**Next steps for deployment:**
```bash
# 1. Create fly.io app
fly launch --name groq-unli --region iad

# 2. Create persistent volume
fly volumes create groq_unli_data --region iad --size 1

# 3. Set secrets
fly secrets set GROQ_UNLI_API_KEY="your-client-key"
fly secrets set GROQ_KEYS="gsk_...,gsk_...,gsk_..."
fly secrets set ADMIN_API_KEY="your-admin-key"

# 4. Deploy
fly deploy
```

**Plan complete and saved to `docs/plans/2025-02-27-groq-unli-implementation.md`.**

**Two execution options:**

**1. Subagent-Driven (this session)** - I dispatch fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** - Open new session with executing-plans, batch execution with checkpoints

**Which approach?**
