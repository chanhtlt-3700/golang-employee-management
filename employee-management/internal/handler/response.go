package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"employee-management/internal/model"
)

// statusClientClosedRequest is the non-standard code nginx uses when the
// client disconnects before the response is ready. Nobody reads that
// response, but it keeps such requests apart from real failures in logs.
const statusClientClosedRequest = 499

// errorResponse is the body of every error response:
//
//	{"error": {"code": "VALIDATION_FAILED", "message": "...", "details": {"age": "..."}}}
type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

// apiFunc is an HTTP handler that returns an error instead of writing the
// error response itself. handle adapts it to http.Handler, so the mapping
// from errors to status codes lives in one place: respondError.
type apiFunc func(w http.ResponseWriter, r *http.Request) error

func handle(logger *slog.Logger, fn apiFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			respondError(w, r, logger, err)
		}
	})
}

// writeJSON encodes v before writing anything, so an encoding failure can
// still become a clean 500 instead of a half-written 200.
func writeJSON(w http.ResponseWriter, status int, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n')) // a write error means the client left; nothing to do
	return nil
}

func writeError(w http.ResponseWriter, status int, code, message string, details map[string]string) {
	_ = writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: message, Details: details}})
}

// respondError maps an error from any layer to an HTTP response.
func respondError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	var reqErr *requestError
	var valErr *model.ValidationError
	switch {
	case errors.As(err, &reqErr):
		writeError(w, reqErr.status, reqErr.code, reqErr.message, nil)
	case errors.As(err, &valErr):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "one or more fields are invalid", valErr.Fields)
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error(), nil)
	case errors.Is(err, model.ErrAlreadyExists):
		writeError(w, http.StatusConflict, "ALREADY_EXISTS", err.Error(), nil)
	case errors.Is(err, context.DeadlineExceeded):
		logger.WarnContext(r.Context(), "request timed out", "method", r.Method, "path", r.URL.Path)
		writeError(w, http.StatusServiceUnavailable, "TIMEOUT", "the request took too long to process", nil)
	case errors.Is(err, context.Canceled):
		writeError(w, statusClientClosedRequest, "CANCELED", "the request was canceled", nil)
	default:
		// Internal details (SQL errors, file paths) are logged, never sent.
		logger.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal server error", nil)
	}
}
