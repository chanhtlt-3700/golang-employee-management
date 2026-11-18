package model

import "time"

// Employee is a stored employee record.
type Employee struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Age          int       `json:"age"`
	Position     string    `json:"position"`
	DepartmentID int64     `json:"departmentId"`
	Salary       float64   `json:"salary"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`

	// DeletedAt is set by a soft delete. Soft-deleted employees are invisible
	// to every read, and the field is never sent to clients.
	DeletedAt *time.Time `json:"-"`
}

// IsDeleted reports whether the employee was soft-deleted.
func (e Employee) IsDeleted() bool { return e.DeletedAt != nil }

// EmployeeDetail is an employee joined with its department. The embedded
// Employee's fields are flattened into the same JSON object.
type EmployeeDetail struct {
	Employee
	DepartmentName string `json:"departmentName"`
}

// CreateEmployeeInput is the body of POST /employees and of PUT
// /employees/{id}, which replaces the whole record.
type CreateEmployeeInput struct {
	Name         string  `json:"name"`
	Age          int     `json:"age"`
	Position     string  `json:"position"`
	DepartmentID int64   `json:"departmentId"`
	Salary       float64 `json:"salary"`
}

// AsUpdate returns an update that sets every field. Create and full
// replacement then share one validation and apply path with partial updates.
func (in CreateEmployeeInput) AsUpdate() UpdateEmployeeInput {
	return UpdateEmployeeInput{
		Name:         &in.Name,
		Age:          &in.Age,
		Position:     &in.Position,
		DepartmentID: &in.DepartmentID,
		Salary:       &in.Salary,
	}
}

// UpdateEmployeeInput is the body of PATCH /employees/{id}. Pointer fields
// tell "absent" (nil, leave unchanged) apart from a zero value such as 0.
// A JSON null also decodes to nil, so it means "unchanged" too.
type UpdateEmployeeInput struct {
	Name         *string  `json:"name"`
	Age          *int     `json:"age"`
	Position     *string  `json:"position"`
	DepartmentID *int64   `json:"departmentId"`
	Salary       *float64 `json:"salary"`
}

// IsEmpty reports whether the update changes nothing.
func (in UpdateEmployeeInput) IsEmpty() bool {
	return in.Name == nil && in.Age == nil && in.Position == nil &&
		in.DepartmentID == nil && in.Salary == nil
}

// ApplyTo copies every non-nil field onto e.
func (in UpdateEmployeeInput) ApplyTo(e *Employee) {
	if in.Name != nil {
		e.Name = *in.Name
	}
	if in.Age != nil {
		e.Age = *in.Age
	}
	if in.Position != nil {
		e.Position = *in.Position
	}
	if in.DepartmentID != nil {
		e.DepartmentID = *in.DepartmentID
	}
	if in.Salary != nil {
		e.Salary = *in.Salary
	}
}

// EmployeeFilter selects which employees a list query returns.
type EmployeeFilter struct {
	// Keyword, when not empty, keeps employees whose name or position
	// contains it, ignoring case.
	Keyword string
	// Limit caps the number of rows returned; 0 or less means no limit.
	Limit  int
	Offset int
}

// EmployeePage is one page of a list or search result.
type EmployeePage struct {
	TotalCount int        `json:"totalCount"`
	Limit      int        `json:"limit"`
	Offset     int        `json:"offset"`
	Employees  []Employee `json:"employees"`
}
