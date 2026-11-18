package memory

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"employee-management/internal/model"
	"employee-management/internal/repository"
)

// DepartmentRepository is the in-memory repository.DepartmentRepository.
type DepartmentRepository struct {
	store *Store
}

var _ repository.DepartmentRepository = (*DepartmentRepository)(nil)

// NewDepartmentRepository returns a repository backed by store.
func NewDepartmentRepository(store *Store) *DepartmentRepository {
	return &DepartmentRepository{store: store}
}

// Create implements repository.DepartmentRepository. The uniqueness check
// and the insert happen under one write lock, so two concurrent requests
// for the same name cannot both succeed.
func (r *DepartmentRepository) Create(ctx context.Context, d model.Department) (model.Department, error) {
	if err := ctx.Err(); err != nil {
		return model.Department{}, err
	}
	key := strings.ToLower(d.Name)

	s := r.store
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, taken := s.departmentByName[key]; taken {
		return model.Department{}, fmt.Errorf("department %q %w", d.Name, model.ErrAlreadyExists)
	}
	s.lastDepartmentID++
	d.ID = s.lastDepartmentID
	s.departments[d.ID] = d
	s.departmentByName[key] = d.ID
	return d, nil
}

// GetByID implements repository.DepartmentRepository.
func (r *DepartmentRepository) GetByID(ctx context.Context, id int64) (model.Department, error) {
	if err := ctx.Err(); err != nil {
		return model.Department{}, err
	}
	s := r.store
	s.mu.RLock()
	defer s.mu.RUnlock()

	d, ok := s.departments[id]
	if !ok {
		return model.Department{}, fmt.Errorf("department %d %w", id, model.ErrNotFound)
	}
	return d, nil
}

// List implements repository.DepartmentRepository.
func (r *DepartmentRepository) List(ctx context.Context) ([]model.Department, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s := r.store
	s.mu.RLock()
	out := make([]model.Department, 0, len(s.departments))
	for _, d := range s.departments {
		out = append(out, d)
	}
	s.mu.RUnlock()

	slices.SortFunc(out, func(a, b model.Department) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}
