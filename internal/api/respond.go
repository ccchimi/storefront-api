package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/ccchimi/storefront-api/internal/ratelimit"
)

// errorBody is the shape of every error response:
//
//	{"error": {"code": "...", "message": "...", "fields": {...}}}
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func newError(code, message string) errorBody {
	return errorBody{Error: errorDetail{Code: code, Message: message}}
}

func validationError(fields map[string]string) errorBody {
	return errorBody{Error: errorDetail{Code: "VALIDATION_ERROR", Message: "Request validation failed", Fields: fields}}
}

// encode marshals a response body. Bodies are plain data, so a failure here
// is a programming error rather than something the client can cause.
func encode(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return append(b, '\n')
}

func writeRaw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeJSON(w http.ResponseWriter, status int, v any) { writeRaw(w, status, encode(v)) }

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, newError(code, message))
}

func writeValidation(w http.ResponseWriter, fields map[string]string) {
	writeJSON(w, http.StatusBadRequest, validationError(fields))
}

func writeInternal(w http.ResponseWriter) {
	writeError(w, http.StatusInternalServerError, "INTERNAL", "Something went wrong")
}

func writeRateLimited(w http.ResponseWriter, retryAfter time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(ratelimit.Seconds(retryAfter)))
	writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests, slow down")
}
