// Command server runs the Employee Management HTTP API.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"employee-management/internal/config"
	"employee-management/internal/handler"
	"employee-management/internal/model"
	"employee-management/internal/repository"
	"employee-management/internal/repository/memory"
	"employee-management/internal/repository/postgres"
	"employee-management/internal/service"
	"employee-management/internal/worker"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("server stopped with error", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// ctx is canceled on Ctrl+C or SIGTERM; that starts the shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repos, err := openRepositories(ctx, cfg)
	if err != nil {
		return err
	}
	defer repos.close()

	// Dependencies are wired by hand: repository -> service -> handler.
	departmentSvc := service.NewDepartmentService(repos.departments, repos.employees)
	employeeSvc := service.NewEmployeeService(repos.employees, repos.departments, service.DeleteMode(cfg.DeleteMode))

	if cfg.SeedData && cfg.Storage == config.StorageMemory {
		if err := seedDemoData(ctx, departmentSvc, employeeSvc); err != nil {
			return fmt.Errorf("seed demo data: %w", err)
		}
		logger.Info("demo data loaded", "departments", 2, "employees", 4)
	}

	exporter := worker.NewExporter(employeeSvc, cfg.ExportDir, cfg.ExportInterval, logger)
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		exporter.Run(ctx)
	}()

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: handler.NewRouter(handler.Services{
			Employees:   employeeSvc,
			Departments: departmentSvc,
			Exports:     exporter,
		}, logger, cfg.RequestTimeout),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      cfg.RequestTimeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr,
			"storage", cfg.Storage, "deleteMode", cfg.DeleteMode)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err = <-serveErr: // could not start, e.g. the port is taken
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}
	stop() // stops the worker; a second Ctrl+C now kills the process at once

	// Stop accepting connections and let in-flight requests finish.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if shutdownErr := srv.Shutdown(shutdownCtx); shutdownErr != nil {
		logger.Error("http shutdown incomplete", "err", shutdownErr)
	}
	workers.Wait()
	logger.Info("server stopped")

	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

type repositories struct {
	employees   repository.EmployeeRepository
	departments repository.DepartmentRepository
	close       func()
}

func openRepositories(ctx context.Context, cfg config.Config) (repositories, error) {
	if cfg.Storage != config.StoragePostgres {
		store := memory.NewStore()
		return repositories{
			employees:   memory.NewEmployeeRepository(store),
			departments: memory.NewDepartmentRepository(store),
			close:       func() {},
		}, nil
	}

	db, err := sql.Open(cfg.DBDriver, cfg.DBDSN)
	if err != nil {
		return repositories{}, fmt.Errorf("open database (is a %q driver imported? see README): %w", cfg.DBDriver, err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(initCtx); err != nil {
		_ = db.Close()
		return repositories{}, fmt.Errorf("connect to database: %w", err)
	}
	if err := postgres.Migrate(initCtx, db); err != nil {
		_ = db.Close()
		return repositories{}, err
	}
	return repositories{
		employees:   postgres.NewEmployeeRepository(db),
		departments: postgres.NewDepartmentRepository(db),
		close:       func() { _ = db.Close() },
	}, nil
}

// seedDemoData goes through the services, so the demo data is validated
// like any client request.
func seedDemoData(ctx context.Context, departments *service.DepartmentService, employees *service.EmployeeService) error {
	eng, err := departments.Create(ctx, model.CreateDepartmentInput{Name: "Engineering"})
	if err != nil {
		return err
	}
	hr, err := departments.Create(ctx, model.CreateDepartmentInput{Name: "Human Resources"})
	if err != nil {
		return err
	}
	for _, in := range []model.CreateEmployeeInput{
		{Name: "Nguyễn Văn An", Age: 28, Position: "Backend Developer", DepartmentID: eng.ID, Salary: 2000},
		{Name: "Trần Thị Bình", Age: 34, Position: "Engineering Manager", DepartmentID: eng.ID, Salary: 3500},
		{Name: "Lê Minh Châu", Age: 25, Position: "Frontend Developer", DepartmentID: eng.ID, Salary: 1800},
		{Name: "Phạm Thu Dung", Age: 31, Position: "HR Specialist", DepartmentID: hr.ID, Salary: 1500},
	} {
		if _, err := employees.Create(ctx, in); err != nil {
			return err
		}
	}
	return nil
}
