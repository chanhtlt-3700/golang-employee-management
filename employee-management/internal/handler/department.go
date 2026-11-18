package handler

import (
	"context"
	"net/http"
	"strconv"

	"employee-management/internal/model"
)

// DepartmentService is what the department handlers need.
type DepartmentService interface {
	Create(ctx context.Context, in model.CreateDepartmentInput) (model.Department, error)
	List(ctx context.Context) ([]model.Department, error)
	ListEmployees(ctx context.Context, id int64) (model.DepartmentEmployees, error)
}

// DepartmentHandler serves /departments.
type DepartmentHandler struct {
	svc DepartmentService
}

// NewDepartmentHandler returns a handler backed by svc.
func NewDepartmentHandler(svc DepartmentService) *DepartmentHandler {
	return &DepartmentHandler{svc: svc}
}

// Create handles POST /departments.
func (h *DepartmentHandler) Create(w http.ResponseWriter, r *http.Request) error {
	var in model.CreateDepartmentInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	d, err := h.svc.Create(r.Context(), in)
	if err != nil {
		return err
	}
	w.Header().Set("Location", "/departments/"+strconv.FormatInt(d.ID, 10))
	return writeJSON(w, http.StatusCreated, d)
}

// List handles GET /departments.
func (h *DepartmentHandler) List(w http.ResponseWriter, r *http.Request) error {
	departments, err := h.svc.List(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, struct {
		TotalCount  int                `json:"totalCount"`
		Departments []model.Department `json:"departments"`
	}{len(departments), departments})
}

// ListEmployees handles GET /departments/{id}/employees.
func (h *DepartmentHandler) ListEmployees(w http.ResponseWriter, r *http.Request) error {
	id, err := parseID(r)
	if err != nil {
		return err
	}
	result, err := h.svc.ListEmployees(r.Context(), id)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, result)
}
