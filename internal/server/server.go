package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"fast-unli/internal/store"
)

type KeyPool interface {
	GetNextKey() (*store.Key, error)
	ReportSuccess(keyID int64)
	ReportFailure(keyID int64, statusCode int)
	RefreshKeys() error
	HealthyCount() int
	EnableKey(keyID int64) error
}

type ProviderClient interface {
	DoRequest(apiKey, path string, body []byte) (*http.Response, error)
	DoRequestWithMethod(apiKey, method, path string, body []byte) (*http.Response, error)
}

type StoreInterface interface {
	GetAllKeys() ([]*store.Key, error)
	GetKeysByProvider(provider string) ([]*store.Key, error)
	AddKey(key *store.Key) (int64, error)
	DeleteKey(id int64) error
	GetKeyByID(id int64) (*store.Key, error)
	UpdateKeyStatus(id int64, status store.KeyStatus, failCount int, cooldownUntil time.Time) error
}

type Config struct {
	APIKey      string
	AdminAPIKey string
	GodModeKey  string
	Pool        KeyPool
	Provider    ProviderClient
	Store       StoreInterface
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
		clientKey := "Bearer " + s.cfg.APIKey
		adminKey := "Bearer " + s.cfg.AdminAPIKey

		// Check for god mode (if configured)
		godModeKey := ""
		if s.cfg.GodModeKey != "" {
			godModeKey = "Bearer " + s.cfg.GodModeKey
		}

		// Allow client key OR admin key OR god mode key
		if auth != clientKey && auth != adminKey && auth != godModeKey {
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
