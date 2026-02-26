package provider

import (
	"bytes"
	"fmt"
	"net/http"
	"time"
)

type ErrorType string

const (
	ErrorTypeSuccess   ErrorType = "success"
	ErrorTypeAuth      ErrorType = "auth"
	ErrorTypeRateLimit ErrorType = "rate_limit"
	ErrorTypeServer    ErrorType = "server"
	ErrorTypeNetwork   ErrorType = "network"
)

type Client struct {
	baseURL string
	client  *http.Client
}

func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) DoRequest(apiKey, path string, body []byte) (*http.Response, error) {
	url := c.baseURL + path
	
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	
	return c.client.Do(req)
}

func (c *Client) DoRequestWithMethod(apiKey, method, path string, body []byte) (*http.Response, error) {
	url := c.baseURL + path
	
	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}
	
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	
	return c.client.Do(req)
}

func ClassifyError(statusCode int) ErrorType {
	switch statusCode {
	case 200, 201:
		return ErrorTypeSuccess
	case 401, 403:
		return ErrorTypeAuth
	case 429:
		return ErrorTypeRateLimit
	case 500, 502, 503, 504:
		return ErrorTypeServer
	default:
		return ErrorTypeNetwork
	}
}

func GetRetryAfter(resp *http.Response, defaultMinutes int) time.Duration {
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter != "" {
		// Try to parse as seconds
		var seconds int
		if _, err := fmt.Sscanf(retryAfter, "%d", &seconds); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return time.Duration(defaultMinutes) * time.Minute
}
