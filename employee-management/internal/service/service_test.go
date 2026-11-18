package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"employee-management/internal/model"
	"employee-management/internal/repository"
	"employee-management/internal/repository/memory"
)

var fixedNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// mockEmployeeRepo stubs only the methods a test needs. The embedded
// interface is nil, so calling a method that was not stubbed panics: an
// unexpected repository call fails the test loudly.
type mockEmployeeRepo struct {
	repository.EmployeeRepository

	listFn       func(ctx context.Context, f model.EmployeeFilter) ([]model.Employee, int, error)
	softDeleteFn func(ctx context.Context, id int64, at time.Time) error
	hardDeleteFn func(ctx context.Context, id int64) error
}

func (m *mockEmployeeRepo) List(ctx context.Context, f model.EmployeeFilter) ([]model.Employee, int, error) {
	return m.listFn(ctx, f)
}

func (m *mockEmployeeRepo) SoftDelete(ctx context.Context, id int64, at time.Time) error {
	return m.softDeleteFn(ctx, id, at)
}

func (m *mockEmployeeRepo) HardDelete(ctx context.Context, id int64) error {
	return m.hardDeleteFn(ctx, id)
}

// newMemoryServices returns services on a fresh in-memory store with a
// fixed clock and one department, "Engineering" (ID 1).
func newMemoryServices(t *testing.T) (*EmployeeService, *DepartmentService) {
	t.Helper()
	store := memory.NewStore()
	emps := memory.NewEmployeeRepository(store)
	depts := memory.NewDepartmentRepository(store)

	deptSvc := NewDepartmentService(depts, emps)
	deptSvc.now = func() time.Time { return fixedNow }
	if _, err := deptSvc.Create(context.Background(), model.CreateDepartmentInput{Name: "Engineering"}); err != nil {
		t.Fatal(err)
	}

	empSvc := NewEmployeeService(emps, depts, SoftDelete)
	empSvc.now = func() time.Time { return fixedNow }
	return empSvc, deptSvc
}

func validInput() model.CreateEmployeeInput {
	return model.CreateEmployeeInput{
		Name: "Nguyễn Văn An", Age: 28, Position: "Backend Developer", DepartmentID: 1, Salary: 2000,
	}
}

func assertValidationField(t *testing.T, err error, field string) {
	t.Helper()
	var verr *model.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("err = %v, want a *model.ValidationError", err)
	}
	if _, ok := verr.Fields[field]; !ok {
		t.Errorf("fields = %v, want a problem with %q", verr.Fields, field)
	}
}

func TestEmployeeService_Create(t *testing.T) {
	svc, _ := newMemoryServices(t)
	in := validInput()
	in.Name = "  Nguyễn Văn An  "

	got, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 1 {
		t.Errorf("ID = %d, want 1", got.ID)
	}
	if got.Name != "Nguyễn Văn An" {
		t.Errorf("name = %q, want it trimmed", got.Name)
	}
	if !got.CreatedAt.Equal(fixedNow) || !got.UpdatedAt.Equal(fixedNow) {
		t.Errorf("timestamps = %v / %v, want %v", got.CreatedAt, got.UpdatedAt, fixedNow)
	}
}

func TestEmployeeService_CreateValidation(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*model.CreateEmployeeInput)
		field  string
	}{
		{"blank name", func(in *model.CreateEmployeeInput) { in.Name = "   " }, "name"},
		{"name too long", func(in *model.CreateEmployeeInput) { in.Name = strings.Repeat("ă", 101) }, "name"},
		{"too young", func(in *model.CreateEmployeeInput) { in.Age = MinAge - 1 }, "age"},
		{"too old", func(in *model.CreateEmployeeInput) { in.Age = MaxAge + 1 }, "age"},
		{"missing position", func(in *model.CreateEmployeeInput) { in.Position = "" }, "position"},
		{"zero salary", func(in *model.CreateEmployeeInput) { in.Salary = 0 }, "salary"},
		{"negative salary", func(in *model.CreateEmployeeInput) { in.Salary = -1 }, "salary"},
		{"missing department", func(in *model.CreateEmployeeInput) { in.DepartmentID = 0 }, "departmentId"},
		{"unknown department", func(in *model.CreateEmployeeInput) { in.DepartmentID = 99 }, "departmentId"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newMemoryServices(t)
			in := validInput()
			tt.modify(&in)
			_, err := svc.Create(context.Background(), in)
			assertValidationField(t, err, tt.field)
		})
	}
}

func TestEmployeeService_CreateReportsAllInvalidFields(t *testing.T) {
	svc, _ := newMemoryServices(t)
	_, err := svc.Create(context.Background(), model.CreateEmployeeInput{})
	for _, field := range []string{"name", "age", "position", "departmentId", "salary"} {
		assertValidationField(t, err, field)
	}
}

func TestEmployeeService_UpdateChangesOnlyGivenFields(t *testing.T) {
	ctx := context.Background()
	svc, _ := newMemoryServices(t)
	created, _ := svc.Create(ctx, validInput())

	later := fixedNow.Add(time.Hour)
	svc.now = func() time.Time { return later }
	salary := 2500.0
	got, err := svc.Update(ctx, created.ID, model.UpdateEmployeeInput{Salary: &salary})
	if err != nil {
		t.Fatal(err)
	}

	want := created
	want.Salary = 2500
	want.UpdatedAt = later
	if got != want {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestEmployeeService_UpdateErrors(t *testing.T) {
	ctx := context.Background()
	svc, _ := newMemoryServices(t)
	created, _ := svc.Create(ctx, validInput())
	badAge := 10
	name := "Someone"

	t.Run("empty patch", func(t *testing.T) {
		_, err := svc.Update(ctx, created.ID, model.UpdateEmployeeInput{})
		assertValidationField(t, err, "body")
	})
	t.Run("invalid field leaves record unchanged", func(t *testing.T) {
		_, err := svc.Update(ctx, created.ID, model.UpdateEmployeeInput{Age: &badAge})
		assertValidationField(t, err, "age")
		if got, _ := svc.Get(ctx, created.ID); got != created {
			t.Errorf("record changed: %+v", got)
		}
	})
	t.Run("unknown employee", func(t *testing.T) {
		_, err := svc.Update(ctx, 999, model.UpdateEmployeeInput{Name: &name})
		if !errors.Is(err, model.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestEmployeeService_ReplaceOverwritesEveryField(t *testing.T) {
	ctx := context.Background()
	svc, deptSvc := newMemoryServices(t)
	created, _ := svc.Create(ctx, validInput())
	hr, _ := deptSvc.Create(ctx, model.CreateDepartmentInput{Name: "HR"})

	in := model.CreateEmployeeInput{Name: "New Name", Age: 40, Position: "HR Lead", DepartmentID: hr.ID, Salary: 4000}
	got, err := svc.Replace(ctx, created.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != in.Name || got.Age != in.Age || got.Position != in.Position ||
		got.DepartmentID != in.DepartmentID || got.Salary != in.Salary {
		t.Errorf("got %+v, want fields of %+v", got, in)
	}

	// PUT needs every field: a missing one is a validation error.
	_, err = svc.Replace(ctx, created.ID, model.CreateEmployeeInput{Name: "Only Name"})
	assertValidationField(t, err, "age")
}

func TestEmployeeService_DeleteFollowsMode(t *testing.T) {
	for _, mode := range []DeleteMode{SoftDelete, HardDelete} {
		t.Run(string(mode), func(t *testing.T) {
			var called DeleteMode
			repo := &mockEmployeeRepo{
				softDeleteFn: func(_ context.Context, id int64, at time.Time) error {
					called = SoftDelete
					if !at.Equal(fixedNow) {
						t.Errorf("deletedAt = %v, want %v", at, fixedNow)
					}
					return nil
				},
				hardDeleteFn: func(_ context.Context, id int64) error {
					called = HardDelete
					return nil
				},
			}
			svc := NewEmployeeService(repo, nil, mode)
			svc.now = func() time.Time { return fixedNow }

			if err := svc.Delete(context.Background(), 7); err != nil {
				t.Fatal(err)
			}
			if called != mode {
				t.Errorf("repository delete called = %q, want %q", called, mode)
			}
		})
	}
}

func TestEmployeeService_ListNormalizesPagination(t *testing.T) {
	tests := []struct {
		name                  string
		limit, offset         int
		wantLimit, wantOffset int
	}{
		{"defaults", 0, 0, DefaultPageLimit, 0},
		{"keeps valid values", 25, 50, 25, 50},
		{"caps limit", 1000, 0, MaxPageLimit, 0},
		{"negative offset", 5, -3, 5, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got model.EmployeeFilter
			repo := &mockEmployeeRepo{
				listFn: func(_ context.Context, f model.EmployeeFilter) ([]model.Employee, int, error) {
					got = f
					return nil, 0, nil
				},
			}
			page, err := NewEmployeeService(repo, nil, SoftDelete).List(context.Background(), tt.limit, tt.offset)
			if err != nil {
				t.Fatal(err)
			}
			if got.Limit != tt.wantLimit || got.Offset != tt.wantOffset {
				t.Errorf("repository got limit=%d offset=%d, want %d/%d", got.Limit, got.Offset, tt.wantLimit, tt.wantOffset)
			}
			if page.Employees == nil {
				t.Error("Employees is nil; it must be an empty slice so JSON shows []")
			}
		})
	}
}

func TestEmployeeService_ListWrapsRepositoryError(t *testing.T) {
	errDB := errors.New("connection refused")
	repo := &mockEmployeeRepo{
		listFn: func(context.Context, model.EmployeeFilter) ([]model.Employee, int, error) {
			return nil, 0, errDB
		},
	}
	_, err := NewEmployeeService(repo, nil, SoftDelete).List(context.Background(), 10, 0)
	if !errors.Is(err, errDB) {
		t.Errorf("err = %v, want it to wrap %v", err, errDB)
	}
}

func TestEmployeeService_SearchRequiresKeyword(t *testing.T) {
	svc, _ := newMemoryServices(t)
	_, err := svc.Search(context.Background(), "   ", 10, 0)
	assertValidationField(t, err, "keyword")
}

func TestEmployeeService_ListAllReadsEveryBatch(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	repo := memory.NewEmployeeRepository(store)
	const n = 2*listAllBatch + 3
	for range n {
		if _, err := repo.Create(ctx, model.Employee{}); err != nil {
			t.Fatal(err)
		}
	}

	all, err := NewEmployeeService(repo, memory.NewDepartmentRepository(store), SoftDelete).ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != n {
		t.Fatalf("got %d employees, want %d", len(all), n)
	}
	for i, e := range all {
		if e.ID != int64(i+1) {
			t.Fatalf("all[%d].ID = %d, want %d", i, e.ID, i+1)
		}
	}
}

func TestDepartmentService_Create(t *testing.T) {
	ctx := context.Background()
	_, svc := newMemoryServices(t) // already holds "Engineering"

	got, err := svc.Create(ctx, model.CreateDepartmentInput{Name: "  Sales  "})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Sales" {
		t.Errorf("name = %q, want it trimmed", got.Name)
	}

	_, err = svc.Create(ctx, model.CreateDepartmentInput{Name: "engineering"})
	if !errors.Is(err, model.ErrAlreadyExists) {
		t.Errorf("duplicate: err = %v, want ErrAlreadyExists", err)
	}

	_, err = svc.Create(ctx, model.CreateDepartmentInput{Name: " "})
	assertValidationField(t, err, "name")
}

func TestDepartmentService_ListEmployees(t *testing.T) {
	ctx := context.Background()
	empSvc, deptSvc := newMemoryServices(t)
	created, _ := empSvc.Create(ctx, validInput())

	got, err := deptSvc.ListEmployees(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Department.Name != "Engineering" || got.TotalCount != 1 {
		t.Fatalf("got %+v", got)
	}
	if e := got.Employees[0]; e.ID != created.ID || e.DepartmentName != "Engineering" {
		t.Errorf("employee = %+v, want ID %d in Engineering", e, created.ID)
	}

	if _, err := deptSvc.ListEmployees(ctx, 42); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("unknown department: err = %v, want ErrNotFound", err)
	}
}
