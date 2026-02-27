package server

import (
	"io"
	"log"
	"net/http"
)

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	// Check provider is configured
	if s.cfg.Provider == nil {
		writeError(w, http.StatusServiceUnavailable, "No provider configured")
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Failed to read body")
		return
	}
	defer r.Body.Close()

	log.Printf("Request body (first 200 chars): %s", string(body)[:min(len(body), 200)])

	// Get next available key
	key, err := s.cfg.Pool.GetNextKey()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "No healthy API keys available")
		return
	}

	// Make request to provider
	log.Printf("Making request to provider with key ID %d, body size: %d bytes", key.ID, len(body))
	resp, err := s.cfg.Provider.DoRequest(key.KeyValue, "/chat/completions", body)
	if err != nil {
		log.Printf("Provider request failed for key %d: %v", key.ID, err)
		s.cfg.Pool.ReportFailure(key.ID, 0) // 0 = network error
		writeError(w, http.StatusBadGateway, "Provider request failed")
		return
	}
	defer resp.Body.Close()

	// Handle error status codes
	if resp.StatusCode != http.StatusOK {
		// Read error response body
		errBody, _ := io.ReadAll(resp.Body)
		log.Printf("Provider returned status %d for key %d: %s", resp.StatusCode, key.ID, string(errBody))

		// Don't retry on 4xx errors (client errors) - the request is bad
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			writeError(w, resp.StatusCode, string(errBody))
			return
		}

		// Try next key for 5xx errors (server errors)
		if s.cfg.Pool.HealthyCount() > 0 {
			// Note: Can't retry with same body since it's already consumed
			// For now, just return the error
			writeError(w, http.StatusServiceUnavailable, "All API keys exhausted")
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
