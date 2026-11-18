// Package repository declares the storage contracts the service layer
// depends on. Implementations live in sub-packages (memory, postgres), and
// tests can substitute their own fakes.
//
// Contract shared by every implementation:
//   - Every method honors ctx: it returns ctx.Err() once ctx is done.
//   - Soft-deleted employees behave as if they did not exist.
//   - A missing record yields an error wrapping model.ErrNotFound.
//   - Values are returned by copy; mutating them never changes storage.
package repository

import (
	"context"
	"time"

	"employee-management/internal/model"
)

// EmployeeRepository stores employees.
type EmployeeRepository interface {
	// Create assigns a new ID to e, stores it, and returns the stored copy.
	Create(ctx context.Context, e model.Employee) (model.Employee, error)

	GetByID(ctx context.Context, id int64) (model.Employee, error)

	// List returns the employees matching f, ordered by ID, together with
	// the total number of matches before Limit and Offset were applied.
	List(ctx context.Context, f model.EmployeeFilter) ([]model.Employee, int, error)

	// Update loads the employee, lets fn modify it, and saves the result as
	// one atomic step: concurrent updates of the same employee run one after
	// another, so none is lost. If fn returns an error, nothing is saved.
	// fn runs while storage is locked, so it must be fast and must not call
	// the repository.
	Update(ctx context.Context, id int64, fn func(*model.Employee) error) (model.Employee, error)

	// SoftDelete marks the employee as deleted at the given time.
	SoftDelete(ctx context.Context, id int64, at time.Time) error

	// HardDelete removes the employee permanently.
	HardDelete(ctx context.Context, id int64) error

	// ListByDepartment joins employees with their department and returns
	// those in departmentID, ordered by ID. A department with no employees
	// (or no such department) gives an empty slice.
	ListByDepartment(ctx context.Context, departmentID int64) ([]model.EmployeeDetail, error)
}

// DepartmentRepository stores departments. Names are unique, ignoring case.
type DepartmentRepository interface {
	// Create assigns a new ID to d and stores it. A name already in use
	// yields an error wrapping model.ErrAlreadyExists.
	Create(ctx context.Context, d model.Department) (model.Department, error)

	GetByID(ctx context.Context, id int64) (model.Department, error)

	// List returns every department, ordered by ID.
	List(ctx context.Context) ([]model.Department, error)
}
