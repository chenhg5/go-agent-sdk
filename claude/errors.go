package claude

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// APIError represents a structured error response from the Claude Messages API.
type APIError struct {
	StatusCode int
	Type       string `json:"type"`
	Message    string `json:"message"`
	RawBody    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("claude api: %d %s — %s", e.StatusCode, e.Type, e.Message)
}

// IsRetryable reports whether the error is likely transient.
func (e *APIError) IsRetryable() bool {
	switch e.StatusCode {
	case http.StatusTooManyRequests,
		http.StatusRequestTimeout,
		http.StatusConflict,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		529: // overloaded
		return true
	}
	return false
}

func parseErrorResponse(resp *http.Response) *APIError {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	apiErr := &APIError{
		StatusCode: resp.StatusCode,
		RawBody:    string(body),
	}

	var envelope struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		apiErr.Type = envelope.Error.Type
		apiErr.Message = envelope.Error.Message
	}
	return apiErr
}
