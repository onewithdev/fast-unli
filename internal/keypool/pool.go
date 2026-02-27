package keypool

import (
	"errors"
	"sync"
	"time"
	
	"fast-unli/internal/store"
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
