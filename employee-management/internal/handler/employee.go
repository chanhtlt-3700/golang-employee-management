// Package handler is the HTTP layer: it parses requests, calls a service,
// and writes JSON responses. It holds no business rules.
package handler

import (
	"context"
	"net/http"
	"strconv"

	"employee-management/internal/model"
)

// EmployeeService is what the employee handlers need. It is declared here,
// where it is used, so tests can pass any fake that has these methods.
type EmployeeService interface {
	Create(ctx context.Context, in model.CreateEmployeeInput) (model.Employee, error)
	Get(ctx context.Context, id int64) (model.Employee, error)
	List(ctx context.Context, limit, offset int) (model.EmployeePage, error)
	Search(ctx context.Context, keyword string, limit, offset int) (model.EmployeePage, error)
	Update(ctx context.Context, id int64, in model.UpdateEmployeeInput) (model.Employee, error)
	Replace(ctx context.Context, id int64, in model.CreateEmployeeInput) (model.Employee, error)
	Delete(ctx context.Context, id int64) error
}

// EmployeeHandler serves /employees.
type EmployeeHandler struct {
	svc EmployeeService
}

// NewEmployeeHandler returns a handler backed by svc.
func NewEmployeeHandler(svc EmployeeService) *EmployeeHandler {
	return &EmployeeHandler{svc: svc}
}

// Create handles POST /employees.
func (h *EmployeeHandler) Create(w http.ResponseWriter, r *http.Request) error {
	var in model.CreateEmployeeInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	e, err := h.svc.Create(r.Context(), in)
	if err != nil {
		return err
	}
	w.Header().Set("Location", "/employees/"+strconv.FormatInt(e.ID, 10))
	return writeJSON(w, http.StatusCreated, e)
}

// List handles GET /employees?limit=&offset=.
func (h *EmployeeHandler) List(w http.ResponseWriter, r *http.Request) error {
	limit, offset, err := parsePagination(r.URL.Query())
	if err != nil {
		return err
	}
	page, err := h.svc.List(r.Context(), limit, offset)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, page)
}

// Search handles GET /employees/search?keyword=&limit=&offset=.
func (h *EmployeeHandler) Search(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	limit, offset, err := parsePagination(q)
	if err != nil {
		return err
	}
	page, err := h.svc.Search(r.Context(), q.Get("keyword"), limit, offset)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, page)
}

// Get handles GET /employees/{id}.
func (h *EmployeeHandler) Get(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	e, err := h.svc.Get(r.Context(), id)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, e)
}

// Replace handles PUT /employees/{id}: every field is required.
func (h *EmployeeHandler) Replace(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	var in model.CreateEmployeeInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	e, err := h.svc.Replace(r.Context(), id, in)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, e)
}

// Patch handles PATCH /employees/{id}: only the fields sent are changed.
func (h *EmployeeHandler) Patch(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	var in model.UpdateEmployeeInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	e, err := h.svc.Update(r.Context(), id, in)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, e)
}

// Delete handles DELETE /employees/{id}.
func (h *EmployeeHandler) Delete(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
