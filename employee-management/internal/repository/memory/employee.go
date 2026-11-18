package memory

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"employee-management/internal/model"
	"employee-management/internal/repository"
)

// EmployeeRepository is the in-memory repository.EmployeeRepository.
type EmployeeRepository struct {
	store *Store
}

var _ repository.EmployeeRepository = (*EmployeeRepository)(nil)

// NewEmployeeRepository returns a repository backed by store.
func NewEmployeeRepository(store *Store) *EmployeeRepository {
	return &EmployeeRepository{store: store}
}

func employeeNotFound(id int64) error {
	return fmt.Errorf("employee %d %w", id, model.ErrNotFound)
}

// Create implements repository.EmployeeRepository.
func (r *EmployeeRepository) Create(ctx context.Context, e model.Employee) (model.Employee, error) {
	if err := ctx.Err(); err != nil {
		return model.Employee{}, err
	}
	s := r.store
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastEmployeeID++
	e.ID = s.lastEmployeeID
	s.employees[e.ID] = e
	return e, nil
}

// GetByID implements repository.EmployeeRepository.
func (r *EmployeeRepository) GetByID(ctx context.Context, id int64) (model.Employee, error) {
	if err := ctx.Err(); err != nil {
		return model.Employee{}, err
	}
	s := r.store
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.employees[id]
	if !ok || e.IsDeleted() {
		return model.Employee{}, employeeNotFound(id)
	}
	return e, nil
}

// List implements repository.EmployeeRepository.
func (r *EmployeeRepository) List(ctx context.Context, f model.EmployeeFilter) ([]model.Employee, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	keyword := strings.ToLower(strings.TrimSpace(f.Keyword))

	s := r.store
	s.mu.RLock()
	matched := make([]model.Employee, 0, len(s.employees))
	for _, e := range s.employees {
		if e.IsDeleted() {
			continue
		}
		if keyword != "" && !containsFold(e.Name, keyword) && !containsFold(e.Position, keyword) {
			continue
		}
		matched = append(matched, e)
	}
	s.mu.RUnlock() // matched is our own copy: sort it without holding the lock

	// Map iteration order is random; sorting keeps pages stable between calls.
	slices.SortFunc(matched, func(a, b model.Employee) int { return cmp.Compare(a.ID, b.ID) })
	return page(matched, f.Offset, f.Limit), len(matched), nil
}

// containsFold reports whether s contains lowerSub, ignoring case.
// lowerSub must already be lower-cased.
func containsFold(s, lowerSub string) bool {
	return strings.Contains(strings.ToLower(s), lowerSub)
}

// Update implements repository.EmployeeRepository. The write lock is held
// from read to save, which is what makes the read-modify-write atomic.
func (r *EmployeeRepository) Update(ctx context.Context, id int64, fn func(*model.Employee) error) (model.Employee, error) {
	if err := ctx.Err(); err != nil {
		return model.Employee{}, err
	}
	s := r.store
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.employees[id]
	if !ok || e.IsDeleted() {
		return model.Employee{}, employeeNotFound(id)
	}
	// fn edits a copy, so if it fails the stored record is untouched.
	if err := fn(&e); err != nil {
		return model.Employee{}, err
	}
	e.ID = id // the identity is not fn's to change
	s.employees[id] = e
	return e, nil
}

// SoftDelete implements repository.EmployeeRepository.
func (r *EmployeeRepository) SoftDelete(ctx context.Context, id int64, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s := r.store
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.employees[id]
	if !ok || e.IsDeleted() {
		return employeeNotFound(id)
	}
	e.DeletedAt = &at
	e.UpdatedAt = at
	s.employees[id] = e
	return nil
}

// HardDelete implements repository.EmployeeRepository.
func (r *EmployeeRepository) HardDelete(ctx context.Context, id int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s := r.store
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.employees[id]
	if !ok || e.IsDeleted() {
		return employeeNotFound(id)
	}
	delete(s.employees, id)
	return nil
}

// ListByDepartment implements repository.EmployeeRepository. It is the
// in-memory equivalent of
//
//	SELECT e.*, d.name FROM employees e
//	JOIN departments d ON d.id = e.department_id
//	WHERE e.department_id = ? AND e.deleted_at IS NULL
//
// done as a hash join: each employee row looks up its department by key.
func (r *EmployeeRepository) ListByDepartment(ctx context.Context, departmentID int64) ([]model.EmployeeDetail, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s := r.store
	s.mu.RLock()
	out := make([]model.EmployeeDetail, 0)
	for _, e := range s.employees {
		if e.IsDeleted() || e.DepartmentID != departmentID {
			continue
		}
		d, ok := s.departments[e.DepartmentID]
		if !ok {
			continue // INNER JOIN semantics: no matching department, no row
		}
		out = append(out, model.EmployeeDetail{Employee: e, DepartmentName: d.Name})
	}
	s.mu.RUnlock()

	slices.SortFunc(out, func(a, b model.EmployeeDetail) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}
