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
