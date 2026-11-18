package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"employee-management/internal/model"
	"employee-management/internal/repository"
)

// EmployeeRepository is the PostgreSQL repository.EmployeeRepository.
type EmployeeRepository struct {
	db *sql.DB
}

var _ repository.EmployeeRepository = (*EmployeeRepository)(nil)

// NewEmployeeRepository returns a repository using db.
func NewEmployeeRepository(db *sql.DB) *EmployeeRepository {
	return &EmployeeRepository{db: db}
}

const employeeColumns = "id, name, age, position, department_id, salary, created_at, updated_at"

func scanEmployee(s scanner) (model.Employee, error) {
	var e model.Employee
	err := s.Scan(&e.ID, &e.Name, &e.Age, &e.Position, &e.DepartmentID, &e.Salary, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

func employeeNotFound(id int64) error {
	return fmt.Errorf("employee %d %w", id, model.ErrNotFound)
}

// Create implements repository.EmployeeRepository.
func (r *EmployeeRepository) Create(ctx context.Context, e model.Employee) (model.Employee, error) {
	const q = `
		INSERT INTO employees (name, age, position, department_id, salary, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`
	err := r.db.QueryRowContext(ctx, q,
		e.Name, e.Age, e.Position, e.DepartmentID, e.Salary, e.CreatedAt, e.UpdatedAt,
	).Scan(&e.ID)
	if err != nil {
		return model.Employee{}, fmt.Errorf("insert employee: %w", err)
	}
	return e, nil
}

// GetByID implements repository.EmployeeRepository.
func (r *EmployeeRepository) GetByID(ctx context.Context, id int64) (model.Employee, error) {
	q := "SELECT " + employeeColumns + " FROM employees WHERE id = $1 AND deleted_at IS NULL"
	e, err := scanEmployee(r.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Employee{}, employeeNotFound(id)
	}
	if err != nil {
		return model.Employee{}, fmt.Errorf("get employee %d: %w", id, err)
	}
	return e, nil
}

// List implements repository.EmployeeRepository. The count and the page
// are read in one REPEATABLE READ transaction so they agree with each other.
func (r *EmployeeRepository) List(ctx context.Context, f model.EmployeeFilter) ([]model.Employee, int, error) {
	where := "deleted_at IS NULL"
	var args []any
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		args = append(args, "%"+likeEscaper.Replace(kw)+"%")
		where += " AND (name ILIKE $1 OR position ILIKE $1)"
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, 0, fmt.Errorf("begin list: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // read-only: rollback is the normal way to end it

	var total int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM employees WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count employees: %w", err)
	}
	if total == 0 || f.Offset >= total {
		return []model.Employee{}, total, nil
	}

	q := "SELECT " + employeeColumns + " FROM employees WHERE " + where + " ORDER BY id"
	if f.Limit > 0 {
		args = append(args, f.Limit)
		q += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if f.Offset > 0 {
		args = append(args, f.Offset)
		q += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list employees: %w", err)
	}
	defer rows.Close()

	employees := make([]model.Employee, 0)
	for rows.Next() {
		e, err := scanEmployee(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan employee: %w", err)
		}
		employees = append(employees, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate employees: %w", err)
	}
	return employees, total, nil
}

// Update implements repository.EmployeeRepository with SELECT ... FOR
// UPDATE: the row stays locked until commit, so concurrent updates of one
// employee run one after another instead of overwriting each other.
func (r *EmployeeRepository) Update(ctx context.Context, id int64, fn func(*model.Employee) error) (model.Employee, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Employee{}, fmt.Errorf("begin update: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once committed

	q := "SELECT " + employeeColumns + " FROM employees WHERE id = $1 AND deleted_at IS NULL FOR UPDATE"
	e, err := scanEmployee(tx.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Employee{}, employeeNotFound(id)
	}
	if err != nil {
		return model.Employee{}, fmt.Errorf("lock employee %d: %w", id, err)
	}

	if err := fn(&e); err != nil {
		return model.Employee{}, err
	}
	e.ID = id

	const upd = `
		UPDATE employees
		SET name = $1, age = $2, position = $3, department_id = $4, salary = $5, updated_at = $6
		WHERE id = $7`
	if _, err := tx.ExecContext(ctx, upd,
		e.Name, e.Age, e.Position, e.DepartmentID, e.Salary, e.UpdatedAt, id,
	); err != nil {
		return model.Employee{}, fmt.Errorf("update employee %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return model.Employee{}, fmt.Errorf("commit update: %w", err)
	}
	return e, nil
}

// SoftDelete implements repository.EmployeeRepository.
func (r *EmployeeRepository) SoftDelete(ctx context.Context, id int64, at time.Time) error {
	res, err := r.db.ExecContext(ctx,
		"UPDATE employees SET deleted_at = $1, updated_at = $1 WHERE id = $2 AND deleted_at IS NULL", at, id)
	return expectOneRow(res, err, id)
}

// HardDelete implements repository.EmployeeRepository.
func (r *EmployeeRepository) HardDelete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM employees WHERE id = $1 AND deleted_at IS NULL", id)
	return expectOneRow(res, err, id)
}

func expectOneRow(res sql.Result, err error, id int64) error {
	if err != nil {
		return fmt.Errorf("delete employee %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete employee %d: %w", id, err)
	}
	if n == 0 {
		return employeeNotFound(id)
	}
	return nil
}

// ListByDepartment implements repository.EmployeeRepository with an INNER
// JOIN, so each row carries its department's name.
func (r *EmployeeRepository) ListByDepartment(ctx context.Context, departmentID int64) ([]model.EmployeeDetail, error) {
	const q = `
		SELECT e.id, e.name, e.age, e.position, e.department_id, e.salary,
		       e.created_at, e.updated_at, d.name
		FROM employees e
		INNER JOIN departments d ON d.id = e.department_id
		WHERE e.department_id = $1 AND e.deleted_at IS NULL
		ORDER BY e.id`
	rows, err := r.db.QueryContext(ctx, q, departmentID)
	if err != nil {
		return nil, fmt.Errorf("list employees of department %d: %w", departmentID, err)
	}
	defer rows.Close()

	out := make([]model.EmployeeDetail, 0)
	for rows.Next() {
		var d model.EmployeeDetail
		if err := rows.Scan(
			&d.ID, &d.Name, &d.Age, &d.Position, &d.DepartmentID, &d.Salary,
			&d.CreatedAt, &d.UpdatedAt, &d.DepartmentName,
		); err != nil {
			return nil, fmt.Errorf("scan employee detail: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate employee details: %w", err)
	}
	return out, nil
}
