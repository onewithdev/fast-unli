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
