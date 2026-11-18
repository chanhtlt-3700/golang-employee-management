// Package memory implements the repository interfaces with maps guarded by a
// mutex. It is the default storage, so the app runs without a database.
package memory

import (
	"sync"

	"employee-management/internal/model"
)

// Store is an in-memory database shared by EmployeeRepository and
// DepartmentRepository.
//
// One sync.RWMutex guards both tables, so a read spanning them (the join in
// ListByDepartment) sees a consistent snapshot, as a SQL query would. Readers
// share the lock; a writer holds it exclusively.
//
// Records are stored and returned by value, so no caller can reach the
// store's copy and change it without holding the lock.
type Store struct {
	mu sync.RWMutex

	employees      map[int64]model.Employee
	lastEmployeeID int64

	departments      map[int64]model.Department
	departmentByName map[string]int64 // lower-cased name -> ID, keeps names unique
	lastDepartmentID int64
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{
		employees:        make(map[int64]model.Employee),
		departments:      make(map[int64]model.Department),
		departmentByName: make(map[string]int64),
	}
}

// page returns items[offset : offset+limit], clamped to the slice bounds.
// A limit of 0 or less means no limit.
func page[T any](items []T, offset, limit int) []T {
	offset = max(offset, 0)
	if offset >= len(items) {
		return []T{}
	}
	end := len(items)
	if limit > 0 {
		end = min(offset+limit, end)
	}
	return items[offset:end]
}
