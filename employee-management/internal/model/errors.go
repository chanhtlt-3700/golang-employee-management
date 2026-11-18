// Package model holds the domain types shared by every layer.
package model

import (
	"errors"
	"sort"
	"strings"
)

// Sentinel errors. Lower layers wrap them with context, for example
// fmt.Errorf("employee %d %w", id, ErrNotFound) gives "employee 7 not found",
// and the HTTP layer maps them to status codes with errors.Is.
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

// ValidationError reports every invalid field at once, so a client can fix
// the whole request in a single round trip.
type ValidationError struct {
	Fields map[string]string
}

// NewValidationError returns an empty ValidationError ready for Add.
func NewValidationError() *ValidationError {
	return &ValidationError{Fields: make(map[string]string)}
}

// Add records a problem with field. The first message per field wins.
func (e *ValidationError) Add(field, message string) {
	if _, exists := e.Fields[field]; !exists {
		e.Fields[field] = message
	}
}

// Err returns nil when no field failed, and e otherwise. It returns the error
// interface rather than *ValidationError so callers never hit Go's
// "typed nil inside a non-nil interface" trap.
func (e *ValidationError) Err() error {
	if len(e.Fields) == 0 {
		return nil
	}
	return e
}

func (e *ValidationError) Error() string {
	fields := make([]string, 0, len(e.Fields))
	for f := range e.Fields {
		fields = append(fields, f)
	}
	sort.Strings(fields)

	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = f + ": " + e.Fields[f]
	}
	return "validation failed: " + strings.Join(parts, "; ")
}
