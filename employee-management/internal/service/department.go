package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"employee-management/internal/model"
	"employee-management/internal/repository"
)

// DepartmentService implements the department use cases.
type DepartmentService struct {
	departments repository.DepartmentRepository
	employees   repository.EmployeeRepository
	now         func() time.Time
}

// NewDepartmentService returns a service.
func NewDepartmentService(
	departments repository.DepartmentRepository,
	employees repository.EmployeeRepository,
) *DepartmentService {
	return &DepartmentService{departments: departments, employees: employees, now: time.Now}
}

// Create validates in and stores a new department. Names are unique,
// ignoring case.
func (s *DepartmentService) Create(ctx context.Context, in model.CreateDepartmentInput) (model.Department, error) {
	name := strings.TrimSpace(in.Name)
	v := model.NewValidationError()
	checkText(v, "name", name)
	if err := v.Err(); err != nil {
		return model.Department{}, err
	}
	return s.departments.Create(ctx, model.Department{Name: name, CreatedAt: s.now().UTC()})
}

// List returns every department.
func (s *DepartmentService) List(ctx context.Context) ([]model.Department, error) {
	departments, err := s.departments.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list departments: %w", err)
	}
	return departments, nil
}

// ListEmployees returns a department and its active employees. It looks the
// department up first, so an unknown ID is a 404 rather than an empty list.
func (s *DepartmentService) ListEmployees(ctx context.Context, id int64) (model.DepartmentEmployees, error) {
	d, err := s.departments.GetByID(ctx, id)
	if err != nil {
		return model.DepartmentEmployees{}, err
	}
	employees, err := s.employees.ListByDepartment(ctx, id)
	if err != nil {
		return model.DepartmentEmployees{}, fmt.Errorf("list employees of department %d: %w", id, err)
	}
	return model.DepartmentEmployees{Department: d, TotalCount: len(employees), Employees: employees}, nil
}
