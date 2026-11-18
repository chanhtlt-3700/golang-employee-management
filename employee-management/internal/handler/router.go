package handler

import (
	"log/slog"
	"net/http"
	"time"

	"employee-management/internal/middleware"
)

// Services groups the dependencies of the HTTP layer.
type Services struct {
	Employees   EmployeeService
	Departments DepartmentService
	Exports     ExportService
}

// NewRouter wires every route and wraps them in the middleware chain.
// requestTimeout bounds each request through its context; 0 disables it.
func NewRouter(svc Services, logger *slog.Logger, requestTimeout time.Duration) http.Handler {
	employees := NewEmployeeHandler(svc.Employees)
	departments := NewDepartmentHandler(svc.Departments)
	exports := NewExportHandler(svc.Exports)

	mux := http.NewServeMux()
	route := func(pattern string, fn apiFunc) { mux.Handle(pattern, handle(logger, fn)) }

	// Go 1.22+ patterns: "METHOD /path/{wildcard}". When two patterns match,
	// the more specific wins, so /employees/search beats /employees/{id}.
	route("POST /employees", employees.Create)
	route("GET /employees", employees.List)
	route("GET /employees/search", employees.Search)
	route("GET /employees/{id}", employees.Get)
	route("PUT /employees/{id}", employees.Replace)
	route("PATCH /employees/{id}", employees.Patch)
	route("DELETE /employees/{id}", employees.Delete)

	route("POST /departments", departments.Create)
	route("GET /departments", departments.List)
	route("GET /departments/{id}/employees", departments.ListEmployees)

	route("POST /exports", exports.Trigger)
	route("GET /exports/latest", exports.Latest)

	route("GET /healthz", func(w http.ResponseWriter, _ *http.Request) error {
		return writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return middleware.Chain(mux,
		middleware.Logging(logger), // outermost: logs the final status, even of a recovered panic
		middleware.Recover(logger),
		middleware.Timeout(requestTimeout),
	)
}
