package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"employee-management/internal/model"
	"employee-management/internal/repository"
)

// DepartmentRepository is the PostgreSQL repository.DepartmentRepository.
type DepartmentRepository struct {
	db *sql.DB
}

var _ repository.DepartmentRepository = (*DepartmentRepository)(nil)

// NewDepartmentRepository returns a repository using db.
func NewDepartmentRepository(db *sql.DB) *DepartmentRepository {
	return &DepartmentRepository{db: db}
}

// Create implements repository.DepartmentRepository. The unique index on
// lower(name) rejects duplicates; ON CONFLICT DO NOTHING turns that into
// "no row returned" instead of a driver-specific error code.
func (r *DepartmentRepository) Create(ctx context.Context, d model.Department) (model.Department, error) {
	const q = `
		INSERT INTO departments (name, created_at) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
		RETURNING id`
	err := r.db.QueryRowContext(ctx, q, d.Name, d.CreatedAt).Scan(&d.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Department{}, fmt.Errorf("department %q %w", d.Name, model.ErrAlreadyExists)
	}
	if err != nil {
		return model.Department{}, fmt.Errorf("insert department: %w", err)
	}
	return d, nil
}

// GetByID implements repository.DepartmentRepository.
func (r *DepartmentRepository) GetByID(ctx context.Context, id int64) (model.Department, error) {
	var d model.Department
	err := r.db.QueryRowContext(ctx,
		"SELECT id, name, created_at FROM departments WHERE id = $1", id,
	).Scan(&d.ID, &d.Name, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Department{}, fmt.Errorf("department %d %w", id, model.ErrNotFound)
	}
	if err != nil {
		return model.Department{}, fmt.Errorf("get department %d: %w", id, err)
	}
	return d, nil
}

// List implements repository.DepartmentRepository.
func (r *DepartmentRepository) List(ctx context.Context) ([]model.Department, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT id, name, created_at FROM departments ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list departments: %w", err)
	}
	defer rows.Close()

	out := make([]model.Department, 0)
	for rows.Next() {
		var d model.Department
		if err := rows.Scan(&d.ID, &d.Name, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan department: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate departments: %w", err)
	}
	return out, nil
}
