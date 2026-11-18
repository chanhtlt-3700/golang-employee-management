package model

import "time"

// Department is a stored department record.
type Department struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// CreateDepartmentInput is the body of POST /departments.
type CreateDepartmentInput struct {
	Name string `json:"name"`
}

// DepartmentEmployees is the response of GET /departments/{id}/employees.
type DepartmentEmployees struct {
	Department Department       `json:"department"`
	TotalCount int              `json:"totalCount"`
	Employees  []EmployeeDetail `json:"employees"`
}
