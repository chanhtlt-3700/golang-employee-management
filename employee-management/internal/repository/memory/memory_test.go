package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"employee-management/internal/model"
)

func TestEmployeeRepository_ConcurrentCreateAssignsUniqueIDs(t *testing.T) {
	repo := NewEmployeeRepository(NewStore())
	const n = 200

	var wg sync.WaitGroup
	ids := make([]int64, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := repo.Create(context.Background(), model.Employee{Name: fmt.Sprintf("employee %d", i)})
			ids[i], errs[i] = e.ID, err
		}()
	}
	wg.Wait()

	seen := make(map[int64]bool, n)
	for i, id := range ids {
		if errs[i] != nil {
			t.Fatalf("create %d: %v", i, errs[i])
		}
		if seen[id] {
			t.Fatalf("id %d assigned twice", id)
		}
		seen[id] = true
	}
	if _, total, _ := repo.List(context.Background(), model.EmployeeFilter{}); total != n {
		t.Errorf("total = %d, want %d", total, n)
	}
}

// With a naive "get, modify, save" sequence, concurrent increments overwrite
// each other. Update runs the callback under the write lock, so none is lost.
func TestEmployeeRepository_ConcurrentUpdatesAreAtomic(t *testing.T) {
	ctx := context.Background()
	repo := NewEmployeeRepository(NewStore())
	e, _ := repo.Create(ctx, model.Employee{Salary: 0})
	const n = 100

	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Update(ctx, e.ID, func(e *model.Employee) error {
				e.Salary++
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	got, _ := repo.GetByID(ctx, e.ID)
	if got.Salary != n {
		t.Errorf("salary = %v, want %d (lost updates)", got.Salary, n)
	}
}

func TestEmployeeRepository_UpdateErrorLeavesRecordUnchanged(t *testing.T) {
	ctx := context.Background()
	repo := NewEmployeeRepository(NewStore())
	e, _ := repo.Create(ctx, model.Employee{Name: "before"})

	errBoom := errors.New("boom")
	_, err := repo.Update(ctx, e.ID, func(e *model.Employee) error {
		e.Name = "after"
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want %v", err, errBoom)
	}
	if got, _ := repo.GetByID(ctx, e.ID); got.Name != "before" {
		t.Errorf("name = %q, want %q", got.Name, "before")
	}
}

func TestEmployeeRepository_ListSearchAndPagination(t *testing.T) {
	ctx := context.Background()
	repo := NewEmployeeRepository(NewStore())
	for _, e := range []model.Employee{
		{Name: "Alice Nguyen", Position: "Backend Developer"},  // 1
		{Name: "Bob Tran", Position: "Frontend Developer"},     // 2
		{Name: "Carol Le", Position: "HR Manager"},             // 3
		{Name: "Dave Pham", Position: "Backend Lead"},          // 4
		{Name: "Nguyễn Văn Ánh", Position: "Data Engineer"},    // 5
		{Name: "Eve 50% Off", Position: "Marketing Associate"}, // 6
	} {
		if _, err := repo.Create(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name      string
		filter    model.EmployeeFilter
		wantIDs   []int64
		wantTotal int
	}{
		{"all", model.EmployeeFilter{}, []int64{1, 2, 3, 4, 5, 6}, 6},
		{"first page", model.EmployeeFilter{Limit: 2}, []int64{1, 2}, 6},
		{"second page", model.EmployeeFilter{Limit: 2, Offset: 2}, []int64{3, 4}, 6},
		{"offset past end", model.EmployeeFilter{Limit: 2, Offset: 10}, []int64{}, 6},
		{"matches position, any case", model.EmployeeFilter{Keyword: "BACKEND"}, []int64{1, 4}, 2},
		{"matches name", model.EmployeeFilter{Keyword: "nguyen"}, []int64{1}, 1},
		{"unicode keyword", model.EmployeeFilter{Keyword: "ánh"}, []int64{5}, 1},
		{"search plus page", model.EmployeeFilter{Keyword: "developer", Limit: 1, Offset: 1}, []int64{2}, 2},
		{"percent is literal", model.EmployeeFilter{Keyword: "50%"}, []int64{6}, 1},
		{"no match", model.EmployeeFilter{Keyword: "zzz"}, []int64{}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, total, err := repo.List(ctx, tt.filter)
			if err != nil {
				t.Fatal(err)
			}
			if total != tt.wantTotal {
				t.Errorf("total = %d, want %d", total, tt.wantTotal)
			}
			gotIDs := make([]int64, len(got))
			for i, e := range got {
				gotIDs[i] = e.ID
			}
			if fmt.Sprint(gotIDs) != fmt.Sprint(tt.wantIDs) {
				t.Errorf("ids = %v, want %v", gotIDs, tt.wantIDs)
			}
		})
	}
}

func TestEmployeeRepository_SoftDelete(t *testing.T) {
	ctx := context.Background()
	store := NewStore()
	repo := NewEmployeeRepository(store)
	e, _ := repo.Create(ctx, model.Employee{Name: "to delete"})

	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := repo.SoftDelete(ctx, e.ID, at); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetByID(ctx, e.ID); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("GetByID after soft delete: err = %v, want ErrNotFound", err)
	}
	if _, total, _ := repo.List(ctx, model.EmployeeFilter{}); total != 0 {
		t.Errorf("List total = %d, want 0", total)
	}
	if err := repo.SoftDelete(ctx, e.ID, at); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("second soft delete: err = %v, want ErrNotFound", err)
	}
	// The row is still stored, only marked.
	if stored := store.employees[e.ID]; stored.DeletedAt == nil || !stored.DeletedAt.Equal(at) {
		t.Errorf("stored DeletedAt = %v, want %v", stored.DeletedAt, at)
	}
}

func TestEmployeeRepository_HardDelete(t *testing.T) {
	ctx := context.Background()
	store := NewStore()
	repo := NewEmployeeRepository(store)
	e, _ := repo.Create(ctx, model.Employee{Name: "to delete"})

	if err := repo.HardDelete(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.employees[e.ID]; ok {
		t.Error("record still stored after hard delete")
	}
	if err := repo.HardDelete(ctx, e.ID); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("second hard delete: err = %v, want ErrNotFound", err)
	}
}

func TestEmployeeRepository_ListByDepartmentJoinsDepartmentName(t *testing.T) {
	ctx := context.Background()
	store := NewStore()
	depts := NewDepartmentRepository(store)
	emps := NewEmployeeRepository(store)

	eng, _ := depts.Create(ctx, model.Department{Name: "Engineering"})
	hr, _ := depts.Create(ctx, model.Department{Name: "HR"})
	a, _ := emps.Create(ctx, model.Employee{Name: "A", DepartmentID: eng.ID})
	_, _ = emps.Create(ctx, model.Employee{Name: "B", DepartmentID: hr.ID})
	c, _ := emps.Create(ctx, model.Employee{Name: "C", DepartmentID: eng.ID})
	gone, _ := emps.Create(ctx, model.Employee{Name: "D", DepartmentID: eng.ID})
	_ = emps.SoftDelete(ctx, gone.ID, time.Now())

	got, err := emps.ListByDepartment(ctx, eng.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != a.ID || got[1].ID != c.ID {
		t.Fatalf("got %+v, want employees %d and %d", got, a.ID, c.ID)
	}
	for _, d := range got {
		if d.DepartmentName != "Engineering" {
			t.Errorf("employee %d: departmentName = %q, want Engineering", d.ID, d.DepartmentName)
		}
	}

	if got, _ := emps.ListByDepartment(ctx, 999); len(got) != 0 {
		t.Errorf("unknown department: got %d rows, want 0", len(got))
	}
}

func TestDepartmentRepository_NamesAreUniqueIgnoringCase(t *testing.T) {
	ctx := context.Background()
	repo := NewDepartmentRepository(NewStore())
	if _, err := repo.Create(ctx, model.Department{Name: "Engineering"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, model.Department{Name: "ENGINEERING"}); !errors.Is(err, model.ErrAlreadyExists) {
		t.Errorf("err = %v, want ErrAlreadyExists", err)
	}
}

// Many goroutines race to create the same department: exactly one wins.
func TestDepartmentRepository_ConcurrentCreateOfSameName(t *testing.T) {
	repo := NewDepartmentRepository(NewStore())
	const n = 50

	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Create(context.Background(), model.Department{Name: "Sales"})
			if err == nil {
				mu.Lock()
				created++
				mu.Unlock()
			} else if !errors.Is(err, model.ErrAlreadyExists) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	if created != 1 {
		t.Errorf("created %d departments, want exactly 1", created)
	}
}

func TestRepositories_StopOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := NewStore()
	emps := NewEmployeeRepository(store)
	depts := NewDepartmentRepository(store)

	checks := map[string]error{}
	_, checks["employee create"] = emps.Create(ctx, model.Employee{})
	_, checks["employee get"] = emps.GetByID(ctx, 1)
	_, _, checks["employee list"] = emps.List(ctx, model.EmployeeFilter{})
	_, checks["employee update"] = emps.Update(ctx, 1, func(*model.Employee) error { return nil })
	checks["employee soft delete"] = emps.SoftDelete(ctx, 1, time.Now())
	checks["employee hard delete"] = emps.HardDelete(ctx, 1)
	_, checks["employee join"] = emps.ListByDepartment(ctx, 1)
	_, checks["department create"] = depts.Create(ctx, model.Department{Name: "x"})
	_, checks["department get"] = depts.GetByID(ctx, 1)
	_, checks["department list"] = depts.List(ctx)

	for name, err := range checks {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s: err = %v, want context.Canceled", name, err)
		}
	}
}
