// Package worker runs background jobs.
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"employee-management/internal/model"
)

// EmployeeSource supplies the employees to export.
type EmployeeSource interface {
	ListAll(ctx context.Context) ([]model.Employee, error)
}

// DefaultTimeout bounds a single export.
const DefaultTimeout = 30 * time.Second

// Exporter writes every employee to a timestamped JSON file, on a fixed
// interval and whenever Trigger is called.
//
// All exports run on the single goroutine that calls Run, so they never
// overlap and the files need no locking. Other goroutines talk to it only
// through the trigger channel and the mutex-guarded last result.
type Exporter struct {
	source   EmployeeSource
	dir      string
	interval time.Duration
	timeout  time.Duration
	logger   *slog.Logger
	now      func() time.Time

	// trigger holds at most one pending request. A Trigger call while one is
	// already pending is dropped: the pending export reads the newest data
	// when it starts, so a second one would only write a duplicate file.
	trigger chan struct{}

	mu   sync.RWMutex
	last *model.ExportResult
}

// NewExporter returns an exporter writing into dir. An interval of 0 or less
// disables scheduled exports; Trigger still works.
func NewExporter(source EmployeeSource, dir string, interval time.Duration, logger *slog.Logger) *Exporter {
	return &Exporter{
		source:   source,
		dir:      dir,
		interval: interval,
		timeout:  DefaultTimeout,
		logger:   logger,
		now:      time.Now,
		trigger:  make(chan struct{}, 1),
	}
}

// Trigger requests an export without waiting for it. It is safe to call
// from any goroutine and never blocks. It reports false if an export was
// already queued.
func (e *Exporter) Trigger() bool {
	select {
	case e.trigger <- struct{}{}:
		return true
	default:
		return false
	}
}

// LastResult returns the most recent export, or false if none ran yet.
func (e *Exporter) LastResult() (model.ExportResult, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.last == nil {
		return model.ExportResult{}, false
	}
	return *e.last, true
}

// Run processes scheduled and triggered exports until ctx is canceled.
// Start it in its own goroutine.
func (e *Exporter) Run(ctx context.Context) {
	var tick <-chan time.Time // stays nil (never fires) when the schedule is off
	if e.interval > 0 {
		ticker := time.NewTicker(e.interval)
		defer ticker.Stop()
		tick = ticker.C
	}

	e.logger.Info("export worker started", "dir", e.dir, "interval", e.interval.String())
	for {
		select {
		case <-ctx.Done():
			e.logger.Info("export worker stopped")
			return
		case <-tick:
			e.runOnce(ctx, model.ExportScheduled)
		case <-e.trigger:
			e.runOnce(ctx, model.ExportManual)
		}
	}
}

func (e *Exporter) runOnce(ctx context.Context, trigger string) {
	// A shutdown should let an export that already started finish (still
	// bounded by e.timeout) instead of leaving a half-done job behind, so
	// the export does not inherit ctx's cancellation.
	res, err := e.Export(context.WithoutCancel(ctx), trigger)
	if err != nil {
		e.logger.Error("export failed", "trigger", trigger, "err", err)
		return
	}
	e.logger.Info("export finished", "trigger", trigger, "file", res.File, "count", res.Count, "durationMs", res.DurationMs)
}

// Export runs one export now, on the calling goroutine, and records the
// result for LastResult. Run calls it; tests may call it directly.
func (e *Exporter) Export(ctx context.Context, trigger string) (model.ExportResult, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	start := time.Now()
	startedAt := e.now().UTC()
	file, count, err := e.write(ctx, startedAt)

	res := model.ExportResult{
		Trigger:    trigger,
		File:       file,
		Count:      count,
		StartedAt:  startedAt,
		DurationMs: time.Since(start).Milliseconds(),
	}
	if err != nil {
		res.Error = err.Error()
	}

	e.mu.Lock()
	e.last = &res
	e.mu.Unlock()
	return res, err
}

// exportDocument is the content of an export file.
type exportDocument struct {
	ExportedAt time.Time        `json:"exportedAt"`
	TotalCount int              `json:"totalCount"`
	Employees  []model.Employee `json:"employees"`
}

func (e *Exporter) write(ctx context.Context, at time.Time) (string, int, error) {
	employees, err := e.source.ListAll(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("load employees: %w", err)
	}
	if err := os.MkdirAll(e.dir, 0o755); err != nil {
		return "", 0, fmt.Errorf("create export dir: %w", err)
	}
	// No ':' in the name: Windows forbids it in file names.
	path := filepath.Join(e.dir, "employees_"+at.Format("20060102-150405.000")+".json")
	doc := exportDocument{ExportedAt: at, TotalCount: len(employees), Employees: employees}
	if err := writeJSONAtomic(path, doc); err != nil {
		return "", 0, err
	}
	return path, len(employees), nil
}

// writeJSONAtomic writes v to a temporary file in the target directory and
// then renames it into place, so nobody ever reads a half-written export.
func writeJSONAtomic(path string, v any) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".export-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err = enc.Encode(v); err != nil {
		return fmt.Errorf("encode export: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("flush export: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close export: %w", err)
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("move export into place: %w", err)
	}
	return nil
}
