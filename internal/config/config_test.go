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
