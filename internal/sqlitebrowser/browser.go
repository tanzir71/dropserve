// Package sqlitebrowser reads SQLite databases for the Dropserve dashboard.
package sqlitebrowser

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	_ "modernc.org/sqlite" // Register the handover-selected pure-Go database/sql driver.
)

const rowLimit = 100

// Snapshot is one lock-free, bounded view of a database file.
type Snapshot struct {
	Path   string  `json:"path"`
	Tables []Table `json:"tables"`
}

// Table contains its column names and at most the first 100 rows.
type Table struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

// Browse opens a database in defensive read-only mode, reads a bounded
// snapshot, and closes every row set and connection before returning.
func Browse(ctx context.Context, path string) (Snapshot, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve SQLite database: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return Snapshot{}, fmt.Errorf("inspect SQLite database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Snapshot{}, fmt.Errorf("SQLite database is not a regular file")
	}
	database, err := sql.Open("sqlite", readOnlyDSN(absolute))
	if err != nil {
		return Snapshot{}, fmt.Errorf("open SQLite database: %w", err)
	}
	database.SetMaxOpenConns(1)
	defer func() { _ = database.Close() }()

	tableRows, err := database.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return Snapshot{}, fmt.Errorf("list SQLite tables: %w", err)
	}
	var names []string
	for tableRows.Next() {
		var name string
		if err := tableRows.Scan(&name); err != nil {
			_ = tableRows.Close()
			return Snapshot{}, fmt.Errorf("read SQLite table name: %w", err)
		}
		names = append(names, name)
	}
	if err := tableRows.Close(); err != nil {
		return Snapshot{}, fmt.Errorf("close SQLite table list: %w", err)
	}
	if err := tableRows.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("list SQLite tables: %w", err)
	}

	snapshot := Snapshot{Path: absolute, Tables: make([]Table, 0, len(names))}
	for _, name := range names {
		table, err := readTable(ctx, database, name)
		if err != nil {
			return Snapshot{}, err
		}
		snapshot.Tables = append(snapshot.Tables, table)
	}
	return snapshot, nil
}

func readTable(ctx context.Context, database *sql.DB, name string) (Table, error) {
	quoted := `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	rows, err := database.QueryContext(ctx, `SELECT * FROM `+quoted+` LIMIT 100`) // #nosec G202 -- identifier is quoted from sqlite_schema, and LIMIT is constant.
	if err != nil {
		return Table{}, fmt.Errorf("read SQLite table %q: %w", name, err)
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		return Table{}, fmt.Errorf("read SQLite columns for %q: %w", name, err)
	}
	table := Table{Name: name, Columns: columns, Rows: make([][]any, 0, rowLimit)}
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return Table{}, fmt.Errorf("read SQLite row from %q: %w", name, err)
		}
		for index, value := range values {
			if binary, ok := value.([]byte); ok {
				values[index] = string(binary)
			}
		}
		table.Rows = append(table.Rows, values)
	}
	if err := rows.Err(); err != nil {
		return Table{}, fmt.Errorf("read SQLite rows from %q: %w", name, err)
	}
	return table, nil
}

func readOnlyDSN(path string) string {
	portable := filepath.ToSlash(path)
	if runtime.GOOS == "windows" && !strings.HasPrefix(portable, "/") {
		portable = "/" + portable
	}
	return (&url.URL{
		Scheme:   "file",
		Path:     portable,
		RawQuery: "mode=ro&_query_only=1&_defensive=1",
	}).String()
}
