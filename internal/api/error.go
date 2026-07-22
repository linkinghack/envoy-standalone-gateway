package api

import (
	"encoding/json"
	"net/http"
)

// ErrorResponse is the stable management API error envelope.
type ErrorResponse struct {
	Error APIError `json:"error"`
}

// APIError is safe to return to management clients.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorResponse{Error: APIError{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
