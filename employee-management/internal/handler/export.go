package handler

import (
	"fmt"
	"net/http"

	"employee-management/internal/model"
)

// ExportService is what the export handlers need from the background worker.
type ExportService interface {
	// Trigger asks for an export without waiting for it. It reports false
	// when one is already queued.
	Trigger() bool
	// LastResult returns the most recent export, or false if none ran yet.
	LastResult() (model.ExportResult, bool)
}

// ExportHandler serves /exports.
type ExportHandler struct {
	svc ExportService
}

// NewExportHandler returns a handler backed by svc.
func NewExportHandler(svc ExportService) *ExportHandler {
	return &ExportHandler{svc: svc}
}

// Trigger handles POST /exports. The export runs in the background, so the
// answer is 202 Accepted; GET /exports/latest shows the outcome.
func (h *ExportHandler) Trigger(w http.ResponseWriter, r *http.Request) error {
	message := "export queued"
	if !h.svc.Trigger() {
		message = "an export is already queued; it will include the latest data"
	}
	return writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted", "message": message})
}

// Latest handles GET /exports/latest.
func (h *ExportHandler) Latest(w http.ResponseWriter, r *http.Request) error {
	result, ok := h.svc.LastResult()
	if !ok {
		return fmt.Errorf("no export has run yet: %w", model.ErrNotFound)
	}
	return writeJSON(w, http.StatusOK, result)
}
