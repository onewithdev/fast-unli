package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"fast-unli/internal/store"
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
