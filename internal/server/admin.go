package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	// Simplified version for now - will be wired to store in Task 7
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
