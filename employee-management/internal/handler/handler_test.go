package handler_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"employee-management/internal/handler"
	"employee-management/internal/model"
	"employee-management/internal/repository/memory"
	"employee-management/internal/service"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

type fakeExporter struct {
	queued bool
	last   *model.ExportResult
}

func (f *fakeExporter) Trigger() bool {
	if f.queued {
		return false
	}
	f.queued = true
	return true
}

func (f *fakeExporter) LastResult() (model.ExportResult, bool) {
	if f.last == nil {
		return model.ExportResult{}, false
	}
	return *f.last, true
}

// newRouter wires the real services to in-memory repositories, so these
// tests cover the whole stack: router -> handler -> service -> repository.
func newRouter(t *testing.T, mode service.DeleteMode) (http.Handler, *fakeExporter) {
	t.Helper()
	store := memory.NewStore()
	emps := memory.NewEmployeeRepository(store)
	depts := memory.NewDepartmentRepository(store)
	exporter := &fakeExporter{}
	return handler.NewRouter(handler.Services{
		Employees:   service.NewEmployeeService(emps, depts, mode),
		Departments: service.NewDepartmentService(depts, emps),
		Exports:     exporter,
	}, discardLogger, time.Second), exporter
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// expect checks the status code and decodes the JSON body into T.
func expect[T any](t *testing.T, rec *httptest.ResponseRecorder, status int) T {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, status, rec.Body)
	}
	var v T
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatalf("decode body %q: %v", rec.Body, err)
		}
	}
	return v
}

type apiError struct {
	Error struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Details map[string]string `json:"details"`
	} `json:"error"`
}

const engineering = `{"name":"Engineering"}`
const alice = `{"name":"Alice Nguyen","age":28,"position":"Backend Developer","departmentId":1,"salary":2000}`

func TestEmployeeLifecycle(t *testing.T) {
	h, _ := newRouter(t, service.SoftDelete)
	expect[model.Department](t, do(t, h, "POST", "/departments", engineering), http.StatusCreated)

	rec := do(t, h, "POST", "/employees", alice)
	created := expect[model.Employee](t, rec, http.StatusCreated)
	if created.ID != 1 || created.Name != "Alice Nguyen" {
		t.Fatalf("created = %+v", created)
	}
	if loc := rec.Header().Get("Location"); loc != "/employees/1" {
		t.Errorf("Location = %q, want /employees/1", loc)
	}

	got := expect[model.Employee](t, do(t, h, "GET", "/employees/1", ""), http.StatusOK)
	if got.Salary != 2000 {
		t.Errorf("salary = %v, want 2000", got.Salary)
	}

	patched := expect[model.Employee](t, do(t, h, "PATCH", "/employees/1", `{"salary":2600}`), http.StatusOK)
	if patched.Salary != 2600 || patched.Name != "Alice Nguyen" || patched.Age != 28 {
		t.Errorf("PATCH changed more than salary: %+v", patched)
	}

	replaced := expect[model.Employee](t, do(t, h, "PUT", "/employees/1",
		`{"name":"Alice N.","age":29,"position":"Tech Lead","departmentId":1,"salary":3000}`), http.StatusOK)
	if replaced.Name != "Alice N." || replaced.Position != "Tech Lead" || replaced.Salary != 3000 {
		t.Errorf("PUT result = %+v", replaced)
	}

	rec = do(t, h, "DELETE", "/employees/1", "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("DELETE: status %d, body %q; want 204 and no body", rec.Code, rec.Body)
	}
	e := expect[apiError](t, do(t, h, "GET", "/employees/1", ""), http.StatusNotFound)
	if e.Error.Code != "NOT_FOUND" || e.Error.Message != "employee 1 not found" {
		t.Errorf("error = %+v", e.Error)
	}
	expect[apiError](t, do(t, h, "DELETE", "/employees/1", ""), http.StatusNotFound)
}

func TestCreateEmployeeRejectsBadInput(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		status      int
		code        string
		detailField string
	}{
		{"empty body", "", 400, "INVALID_JSON", ""},
		{"malformed JSON", `{"name":`, 400, "INVALID_JSON", ""},
		{"wrong type", `{"name":"A","age":"old"}`, 400, "INVALID_JSON", ""},
		{"unknown field", `{"name":"A","salaryy":1}`, 400, "INVALID_JSON", ""},
		{"array body", `[1,2]`, 400, "INVALID_JSON", ""},
		{"two objects", alice + alice, 400, "INVALID_JSON", ""},
		{"age out of range", strings.Replace(alice, `"age":28`, `"age":15`, 1), 400, "VALIDATION_FAILED", "age"},
		{"unknown department", strings.Replace(alice, `"departmentId":1`, `"departmentId":9`, 1), 400, "VALIDATION_FAILED", "departmentId"},
		{"too large", `{"name":"` + strings.Repeat("a", 2<<20) + `"}`, 413, "BODY_TOO_LARGE", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newRouter(t, service.SoftDelete)
			do(t, h, "POST", "/departments", engineering)

			e := expect[apiError](t, do(t, h, "POST", "/employees", tt.body), tt.status)
			if e.Error.Code != tt.code {
				t.Errorf("code = %q, want %q (message %q)", e.Error.Code, tt.code, e.Error.Message)
			}
			if tt.detailField != "" && e.Error.Details[tt.detailField] == "" {
				t.Errorf("details = %v, want an entry for %q", e.Error.Details, tt.detailField)
			}
		})
	}
}

func TestInvalidIDAndQuery(t *testing.T) {
	h, _ := newRouter(t, service.SoftDelete)
	for _, tt := range []struct{ method, path, code string }{
		{"GET", "/employees/abc", "INVALID_ID"},
		{"GET", "/employees/0", "INVALID_ID"},
		{"DELETE", "/employees/-5", "INVALID_ID"},
		{"GET", "/departments/x/employees", "INVALID_ID"},
		{"GET", "/employees?limit=abc", "INVALID_QUERY"},
		{"GET", "/employees?offset=-1", "INVALID_QUERY"},
	} {
		e := expect[apiError](t, do(t, h, tt.method, tt.path, ""), http.StatusBadRequest)
		if e.Error.Code != tt.code {
			t.Errorf("%s %s: code = %q, want %q", tt.method, tt.path, e.Error.Code, tt.code)
		}
	}
}

func TestListEmployeesPaginates(t *testing.T) {
	h, _ := newRouter(t, service.SoftDelete)
	do(t, h, "POST", "/departments", engineering)
	for range 15 {
		expect[model.Employee](t, do(t, h, "POST", "/employees", alice), http.StatusCreated)
	}

	tests := []struct {
		query            string
		wantLen, wantMin int64
		wantLimit        int
	}{
		{"", 10, 1, 10},
		{"?limit=10&offset=10", 5, 11, 10},
		{"?limit=500", 15, 1, 100}, // capped at the maximum
		{"?offset=100", 0, 0, 10},
	}
	for _, tt := range tests {
		page := expect[model.EmployeePage](t, do(t, h, "GET", "/employees"+tt.query, ""), http.StatusOK)
		if page.TotalCount != 15 || int64(len(page.Employees)) != tt.wantLen || page.Limit != tt.wantLimit {
			t.Errorf("%q: total=%d len=%d limit=%d, want 15/%d/%d",
				tt.query, page.TotalCount, len(page.Employees), page.Limit, tt.wantLen, tt.wantLimit)
		}
		if tt.wantLen > 0 && page.Employees[0].ID != tt.wantMin {
			t.Errorf("%q: first ID = %d, want %d", tt.query, page.Employees[0].ID, tt.wantMin)
		}
	}

	// An empty page is [] in JSON, not null.
	if body := do(t, h, "GET", "/employees?offset=100", "").Body.String(); !strings.Contains(body, `"employees":[]`) {
		t.Errorf("empty page body = %s", body)
	}
}

func TestSearchEmployees(t *testing.T) {
	h, _ := newRouter(t, service.SoftDelete)
	do(t, h, "POST", "/departments", engineering)
	for _, body := range []string{
		alice,
		`{"name":"Bob Tran","age":30,"position":"Frontend Developer","departmentId":1,"salary":1800}`,
		`{"name":"Carol Le","age":41,"position":"Backend Lead","departmentId":1,"salary":3800}`,
	} {
		expect[model.Employee](t, do(t, h, "POST", "/employees", body), http.StatusCreated)
	}

	for _, tt := range []struct {
		keyword string
		want    []string
	}{
		{"backend", []string{"Alice Nguyen", "Carol Le"}}, // position
		{"TRAN", []string{"Bob Tran"}},                    // name, any case
		{"designer", []string{}},
	} {
		page := expect[model.EmployeePage](t, do(t, h, "GET", "/employees/search?keyword="+tt.keyword, ""), http.StatusOK)
		names := []string{}
		for _, e := range page.Employees {
			names = append(names, e.Name)
		}
		if strings.Join(names, ",") != strings.Join(tt.want, ",") || page.TotalCount != len(tt.want) {
			t.Errorf("keyword %q: got %v (total %d), want %v", tt.keyword, names, page.TotalCount, tt.want)
		}
	}

	e := expect[apiError](t, do(t, h, "GET", "/employees/search?keyword=", ""), http.StatusBadRequest)
	if e.Error.Details["keyword"] == "" {
		t.Errorf("missing keyword: details = %v", e.Error.Details)
	}
}

func TestDepartments(t *testing.T) {
	h, _ := newRouter(t, service.SoftDelete)

	created := expect[model.Department](t, do(t, h, "POST", "/departments", engineering), http.StatusCreated)
	if created.ID != 1 || created.Name != "Engineering" {
		t.Fatalf("created = %+v", created)
	}
	e := expect[apiError](t, do(t, h, "POST", "/departments", `{"name":"engineering"}`), http.StatusConflict)
	if e.Error.Code != "ALREADY_EXISTS" {
		t.Errorf("duplicate: code = %q", e.Error.Code)
	}
	expect[apiError](t, do(t, h, "POST", "/departments", `{"name":""}`), http.StatusBadRequest)
	expect[model.Department](t, do(t, h, "POST", "/departments", `{"name":"Sales"}`), http.StatusCreated)

	list := expect[struct {
		TotalCount  int                `json:"totalCount"`
		Departments []model.Department `json:"departments"`
	}](t, do(t, h, "GET", "/departments", ""), http.StatusOK)
	if list.TotalCount != 2 || len(list.Departments) != 2 {
		t.Errorf("list = %+v", list)
	}

	expect[model.Employee](t, do(t, h, "POST", "/employees", alice), http.StatusCreated)
	joined := expect[model.DepartmentEmployees](t, do(t, h, "GET", "/departments/1/employees", ""), http.StatusOK)
	if joined.TotalCount != 1 || joined.Employees[0].DepartmentName != "Engineering" {
		t.Errorf("joined = %+v", joined)
	}
	empty := expect[model.DepartmentEmployees](t, do(t, h, "GET", "/departments/2/employees", ""), http.StatusOK)
	if empty.TotalCount != 0 || empty.Employees == nil {
		t.Errorf("empty department = %+v", empty)
	}
	expect[apiError](t, do(t, h, "GET", "/departments/99/employees", ""), http.StatusNotFound)
}

func TestExports(t *testing.T) {
	h, exporter := newRouter(t, service.SoftDelete)

	expect[apiError](t, do(t, h, "GET", "/exports/latest", ""), http.StatusNotFound)

	first := expect[map[string]string](t, do(t, h, "POST", "/exports", ""), http.StatusAccepted)
	second := expect[map[string]string](t, do(t, h, "POST", "/exports", ""), http.StatusAccepted)
	if first["message"] == second["message"] {
		t.Errorf("second trigger should say an export is already queued, got %q", second["message"])
	}

	exporter.last = &model.ExportResult{Trigger: model.ExportManual, File: "exports/x.json", Count: 3}
	got := expect[model.ExportResult](t, do(t, h, "GET", "/exports/latest", ""), http.StatusOK)
	if got.Count != 3 || got.File != "exports/x.json" {
		t.Errorf("latest = %+v", got)
	}
}

// blockingEmployees stands in for a slow dependency. Embedding the interface
// lets it implement only the methods this test calls.
type blockingEmployees struct{ handler.EmployeeService }

func (blockingEmployees) Get(ctx context.Context, _ int64) (model.Employee, error) {
	<-ctx.Done() // wait until the request's deadline passes
	return model.Employee{}, ctx.Err()
}

func (blockingEmployees) List(context.Context, int, int) (model.EmployeePage, error) {
	panic("something went badly wrong")
}

func TestTimeoutAndPanicRecovery(t *testing.T) {
	h := handler.NewRouter(handler.Services{Employees: blockingEmployees{}}, discardLogger, 50*time.Millisecond)

	start := time.Now()
	e := expect[apiError](t, do(t, h, "GET", "/employees/1", ""), http.StatusServiceUnavailable)
	if e.Error.Code != "TIMEOUT" {
		t.Errorf("code = %q, want TIMEOUT", e.Error.Code)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("request took %v; the 50ms deadline did not reach the service", elapsed)
	}

	e = expect[apiError](t, do(t, h, "GET", "/employees", ""), http.StatusInternalServerError)
	if e.Error.Code != "INTERNAL" {
		t.Errorf("code = %q, want INTERNAL", e.Error.Code)
	}
}

func TestHardDeleteMode(t *testing.T) {
	h, _ := newRouter(t, service.HardDelete)
	do(t, h, "POST", "/departments", engineering)
	expect[model.Employee](t, do(t, h, "POST", "/employees", alice), http.StatusCreated)

	if rec := do(t, h, "DELETE", "/employees/1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d", rec.Code)
	}
	expect[apiError](t, do(t, h, "GET", "/employees/1", ""), http.StatusNotFound)
}
