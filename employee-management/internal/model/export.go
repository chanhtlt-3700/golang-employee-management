package model

import "time"

// What started an export.
const (
	ExportScheduled = "scheduled"
	ExportManual    = "manual"
)

// ExportResult describes one run of the background export job.
type ExportResult struct {
	Trigger    string    `json:"trigger"`
	File       string    `json:"file,omitempty"`
	Count      int       `json:"count"`
	StartedAt  time.Time `json:"startedAt"`
	DurationMs int64     `json:"durationMs"`
	Error      string    `json:"error,omitempty"`
}
