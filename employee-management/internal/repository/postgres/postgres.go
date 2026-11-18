// Package postgres implements the repository interfaces on database/sql with
// PostgreSQL syntax ($1 placeholders, RETURNING, ILIKE, FOR UPDATE).
//
// The package itself uses only the standard library. database/sql still
// needs a driver registered by the program, for example
//
//	import _ "github.com/jackc/pgx/v5/stdlib" // registers driver "pgx"
//
// The README explains how to enable it.
package postgres

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed schema.sql
var schema string

// Migrate creates the tables and indexes if they do not exist yet.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	return nil
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// likeEscaper escapes LIKE wildcards so a search for "50%" matches the
// literal text instead of "50 followed by anything".
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
