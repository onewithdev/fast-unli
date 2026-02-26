package provider

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_DoRequest_Success(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Expected auth header, got %v", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"content":"hello"}}]}`))
	}))
	defer server.Close()
	
	client := New(server.URL, 30*time.Second)
	
	body := `{"model":"test","messages":[{"role":"user","content":"hi"}]}`
	resp, err := client.DoRequest("test-key", "/chat/completions", []byte(body))
	if err != nil {
		t.Fatalf("DoRequest() error = %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %v, want 200", resp.StatusCode)
	}
	
	respBody, _ := io.ReadAll(resp.Body)
	if string(respBody) != `{"choices":[{"message":{"content":"hello"}}]}` {
		t.Errorf("body = %v", string(respBody))
	}
}

func TestClient_DoRequest_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Header().Set("Retry-After", "60")
	}))
	defer server.Close()
	
	client := New(server.URL, 30*time.Second)
	
	body := `{"model":"test"}`
	resp, err := client.DoRequest("test-key", "/chat/completions", []byte(body))
	if err != nil {
		t.Fatalf("DoRequest() error = %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %v, want 429", resp.StatusCode)
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		statusCode int
		wantErr    ErrorType
	}{
		{200, ErrorTypeSuccess},
		{401, ErrorTypeAuth},
		{403, ErrorTypeAuth},
		{429, ErrorTypeRateLimit},
		{500, ErrorTypeServer},
		{502, ErrorTypeServer},
		{503, ErrorTypeServer},
	}
	
	for _, tt := range tests {
		t.Run(string(tt.wantErr), func(t *testing.T) {
			got := ClassifyError(tt.statusCode)
			if got != tt.wantErr {
				t.Errorf("ClassifyError(%d) = %v, want %v", tt.statusCode, got, tt.wantErr)
			}
		})
	}
}
