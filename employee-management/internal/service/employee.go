// Package service holds the business rules. It knows nothing about HTTP and
// reaches storage only through the repository interfaces.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"employee-management/internal/model"
	"employee-management/internal/repository"
)

// DeleteMode selects what deleting an employee does.
type DeleteMode string

const (
	SoftDelete DeleteMode = "soft" // mark the record deleted, keep it in storage
	HardDelete DeleteMode = "hard" // remove the record
)

// Pagination bounds.
const (
	DefaultPageLimit = 10
	MaxPageLimit     = 100
)

// Business rules for employee fields.
const (
	MinAge     = 18
	MaxAge     = 65
	maxTextLen = 100

	// listAllBatch is the page size ListAll reads the repository with.
	listAllBatch = 500
)

// EmployeeService implements the employee use cases.
type EmployeeService struct {
	employees   repository.EmployeeRepository
	departments repository.DepartmentRepository
	deleteMode  DeleteMode
	now         func() time.Time // replaced by tests for deterministic timestamps
}

// NewEmployeeService returns a service. Any mode other than HardDelete
// behaves as SoftDelete, the safer choice.
func NewEmployeeService(
	employees repository.EmployeeRepository,
	departments repository.DepartmentRepository,
	mode DeleteMode,
) *EmployeeService {
	return &EmployeeService{
		employees:   employees,
		departments: departments,
		deleteMode:  mode,
		now:         time.Now,
	}
}

// Create validates in and stores a new employee.
func (s *EmployeeService) Create(ctx context.Context, in model.CreateEmployeeInput) (model.Employee, error) {
	fields := normalize(in.AsUpdate())
	if err := s.validate(ctx, fields); err != nil {
		return model.Employee{}, err
	}
	now := s.now().UTC()
	e := model.Employee{CreatedAt: now, UpdatedAt: now}
	fields.ApplyTo(&e)
	return s.employees.Create(ctx, e)
}

// Get returns one employee.
func (s *EmployeeService) Get(ctx context.Context, id int64) (model.Employee, error) {
	return s.employees.GetByID(ctx, id)
}

// List returns one page of employees ordered by ID.
func (s *EmployeeService) List(ctx context.Context, limit, offset int) (model.EmployeePage, error) {
	return s.page(ctx, model.EmployeeFilter{Limit: limit, Offset: offset})
}

// Search returns one page of employees whose name or position contains
// keyword, ignoring case.
func (s *EmployeeService) Search(ctx context.Context, keyword string, limit, offset int) (model.EmployeePage, error) {
	keyword = strings.TrimSpace(keyword)
	v := model.NewValidationError()
	checkText(v, "keyword", keyword)
	if err := v.Err(); err != nil {
		return model.EmployeePage{}, err
	}
	return s.page(ctx, model.EmployeeFilter{Keyword: keyword, Limit: limit, Offset: offset})
}

func (s *EmployeeService) page(ctx context.Context, f model.EmployeeFilter) (model.EmployeePage, error) {
	f.Limit, f.Offset = normalizePage(f.Limit, f.Offset)
	employees, total, err := s.employees.List(ctx, f)
	if err != nil {
		return model.EmployeePage{}, fmt.Errorf("list employees: %w", err)
	}
	if employees == nil {
		employees = []model.Employee{} // encode as [] rather than null
	}
	return model.EmployeePage{TotalCount: total, Limit: f.Limit, Offset: f.Offset, Employees: employees}, nil
}

// normalizePage applies the default and maximum page size.
func normalizePage(limit, offset int) (int, int) {
	switch {
	case limit <= 0:
		limit = DefaultPageLimit
	case limit > MaxPageLimit:
		limit = MaxPageLimit
	}
	return limit, max(offset, 0)
}

// ListAll returns every active employee. It reads the repository in
// batches so no single query has to return an unbounded result set.
func (s *EmployeeService) ListAll(ctx context.Context) ([]model.Employee, error) {
	all := []model.Employee{}
	for offset := 0; ; offset += listAllBatch {
		batch, total, err := s.employees.List(ctx, model.EmployeeFilter{Limit: listAllBatch, Offset: offset})
		if err != nil {
			return nil, fmt.Errorf("list employees from offset %d: %w", offset, err)
		}
		all = append(all, batch...)
		if len(batch) < listAllBatch || offset+listAllBatch >= total {
			return all, nil
		}
	}
}

// Update applies a partial update (PATCH): only the non-nil fields change.
func (s *EmployeeService) Update(ctx context.Context, id int64, in model.UpdateEmployeeInput) (model.Employee, error) {
	if in.IsEmpty() {
		v := model.NewValidationError()
		v.Add("body", "at least one field must be provided")
		return model.Employee{}, v
	}
	return s.update(ctx, id, in)
}

// Replace overwrites every editable field (PUT).
func (s *EmployeeService) Replace(ctx context.Context, id int64, in model.CreateEmployeeInput) (model.Employee, error) {
	return s.update(ctx, id, in.AsUpdate())
}

func (s *EmployeeService) update(ctx context.Context, id int64, in model.UpdateEmployeeInput) (model.Employee, error) {
	in = normalize(in)
	if err := s.validate(ctx, in); err != nil {
		return model.Employee{}, err
	}
	// Everything slow (validation, the department lookup) is done above.
	// The callback runs while the repository holds its lock, so it only
	// copies fields.
	return s.employees.Update(ctx, id, func(e *model.Employee) error {
		in.ApplyTo(e)
		e.UpdatedAt = s.now().UTC()
		return nil
	})
}

// Delete removes an employee according to the configured DeleteMode.
func (s *EmployeeService) Delete(ctx context.Context, id int64) error {
	if s.deleteMode == HardDelete {
		return s.employees.HardDelete(ctx, id)
	}
	return s.employees.SoftDelete(ctx, id, s.now().UTC())
}

// validate checks every non-nil field of in.
func (s *EmployeeService) validate(ctx context.Context, in model.UpdateEmployeeInput) error {
	v := model.NewValidationError()
	if in.Name != nil {
		checkText(v, "name", *in.Name)
	}
	if in.Age != nil && (*in.Age < MinAge || *in.Age > MaxAge) {
		v.Add("age", fmt.Sprintf("must be between %d and %d", MinAge, MaxAge))
	}
	if in.Position != nil {
		checkText(v, "position", *in.Position)
	}
	if in.Salary != nil && *in.Salary <= 0 {
		v.Add("salary", "must be greater than 0")
	}
	if in.DepartmentID != nil {
		if err := s.checkDepartment(ctx, v, *in.DepartmentID); err != nil {
			return err
		}
	}
	return v.Err()
}

// checkDepartment records a validation problem if id does not name an
// existing department. It returns an error only for infrastructure
// failures, which are not the client's fault.
func (s *EmployeeService) checkDepartment(ctx context.Context, v *model.ValidationError, id int64) error {
	if id <= 0 {
		v.Add("departmentId", "must be a positive integer")
		return nil
	}
	_, err := s.departments.GetByID(ctx, id)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, model.ErrNotFound):
		v.Add("departmentId", fmt.Sprintf("department %d does not exist", id))
		return nil
	default:
		return fmt.Errorf("check department %d: %w", id, err)
	}
}

// checkText requires a non-empty value of at most maxTextLen characters.
// It counts runes, not bytes, so "Nguyễn" is 6 characters long.
func checkText(v *model.ValidationError, field, value string) {
	switch n := utf8.RuneCountInString(value); {
	case n == 0:
		v.Add(field, "is required")
	case n > maxTextLen:
		v.Add(field, fmt.Sprintf("must be at most %d characters", maxTextLen))
	}
}

// normalize trims surrounding whitespace from the text fields.
func normalize(in model.UpdateEmployeeInput) model.UpdateEmployeeInput {
	in.Name = trimmed(in.Name)
	in.Position = trimmed(in.Position)
	return in
}

func trimmed(p *string) *string {
	if p == nil {
		return nil
	}
	t := strings.TrimSpace(*p)
	return &t
}
