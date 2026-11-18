package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"employee-management/internal/model"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

type fakeSource struct {
	employees []model.Employee
	err       error
}

func (f fakeSource) ListAll(context.Context) ([]model.Employee, error) {
	return f.employees, f.err
}

func TestExporter_ExportWritesJSONFile(t *testing.T) {
	dir := t.TempDir()
	src := fakeSource{employees: []model.Employee{{ID: 1, Name: "An"}, {ID: 2, Name: "Bình"}}}
	exp := NewExporter(src, dir, 0, discardLogger)

	res, err := exp.Export(context.Background(), model.ExportManual)
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 2 || res.Trigger != model.ExportManual || res.Error != "" {
		t.Errorf("result = %+v", res)
	}

	raw, err := os.ReadFile(res.File)
	if err != nil {
		t.Fatal(err)
	}
	var doc exportDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("export is not valid JSON: %v", err)
	}
	if doc.TotalCount != 2 || doc.Employees[1].Name != "Bình" {
		t.Errorf("document = %+v", doc)
	}

	// Only the final file remains: the temp file was renamed, not copied.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || filepath.Join(dir, entries[0].Name()) != res.File {
		t.Errorf("dir holds %v, want only %s", entries, filepath.Base(res.File))
	}

	if last, ok := exp.LastResult(); !ok || last != res {
		t.Errorf("LastResult = %+v, %v; want %+v", last, ok, res)
	}
}

func TestExporter_ExportRecordsFailure(t *testing.T) {
	errDB := errors.New("database down")
	exp := NewExporter(fakeSource{err: errDB}, t.TempDir(), 0, discardLogger)

	_, err := exp.Export(context.Background(), model.ExportScheduled)
	if !errors.Is(err, errDB) {
		t.Fatalf("err = %v, want it to wrap %v", err, errDB)
	}
	if last, ok := exp.LastResult(); !ok || last.Error == "" || last.File != "" {
		t.Errorf("LastResult = %+v, want a recorded error and no file", last)
	}
}

func TestExporter_TriggerCoalesces(t *testing.T) {
	exp := NewExporter(fakeSource{}, t.TempDir(), 0, discardLogger)
	if !exp.Trigger() {
		t.Error("first Trigger should queue an export")
	}
	if exp.Trigger() {
		t.Error("second Trigger should be dropped while one is queued")
	}
}

// waitForResult polls LastResult until it is set or the deadline passes.
func waitForResult(t *testing.T, exp *Exporter) model.ExportResult {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if res, ok := exp.LastResult(); ok {
			return res
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no export finished in time")
	return model.ExportResult{}
}

// runWorker starts Run in a goroutine and stops it when the test ends.
func runWorker(t *testing.T, exp *Exporter) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		exp.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("worker did not stop after cancel")
		}
	})
}

func TestExporter_RunHandlesTrigger(t *testing.T) {
	exp := NewExporter(fakeSource{employees: []model.Employee{{ID: 1}}}, t.TempDir(), 0, discardLogger)
	runWorker(t, exp)

	exp.Trigger()
	if res := waitForResult(t, exp); res.Trigger != model.ExportManual || res.Count != 1 {
		t.Errorf("result = %+v", res)
	}
}

func TestExporter_RunExportsOnSchedule(t *testing.T) {
	exp := NewExporter(fakeSource{}, t.TempDir(), 20*time.Millisecond, discardLogger)
	runWorker(t, exp)

	if res := waitForResult(t, exp); res.Trigger != model.ExportScheduled {
		t.Errorf("trigger = %q, want %q", res.Trigger, model.ExportScheduled)
	}
}
