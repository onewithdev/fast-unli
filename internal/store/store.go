package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
	
	_ "modernc.org/sqlite"
)

var ErrNoHealthyKeys = errors.New("no healthy API keys available")

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
