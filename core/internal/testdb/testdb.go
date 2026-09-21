// Package testdb gives each test its own MariaDB database with every core migration applied.
package testdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// DSNVariable names the environment variable holding a DSN for a user that can create databases.
const DSNVariable = "CENTRAL_TEST_DSN"

// Database is one freshly migrated database that lives for a single test.
type Database struct {
	Host     string
	Port     string
	Name     string
	User     string
	Password string
	// SQL runs arrange and assert queries against the database, several statements per call if needed.
	SQL *sql.DB
}

// New creates a database with a random name, applies every up migration in name order,
// and drops it when the test ends. It skips the test when DSNVariable is unset.
func New(t *testing.T) *Database {
	t.Helper()
	dsn := os.Getenv(DSNVariable)
	if dsn == "" {
		t.Skipf("%s is unset, so the MariaDB tests are skipped. Point it at a user that can create databases.", DSNVariable)
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse %s: %v", DSNVariable, err)
	}
	host, port, err := net.SplitHostPort(config.Addr)
	if err != nil {
		t.Fatalf("split %s address %q: %v", DSNVariable, config.Addr, err)
	}

	name := "washgate_test_" + strings.ToLower(rand.Text())
	server := openConnection(t, config, "")
	if _, err := server.ExecContext(t.Context(), "CREATE DATABASE `"+name+"`"); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		// t.Context is already canceled when cleanup runs.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := server.ExecContext(ctx, "DROP DATABASE IF EXISTS `"+name+"`"); err != nil {
			t.Errorf("drop test database: %v", err)
		}
	})

	database := &Database{
		Host:     host,
		Port:     port,
		Name:     name,
		User:     config.User,
		Password: config.Passwd,
		SQL:      openConnection(t, config, name),
	}
	database.apply(t, migrationFiles(t, ".up.sql"))
	return database
}

// RollBack applies every down migration in the reverse of name order.
func (d *Database) RollBack(t *testing.T) {
	t.Helper()
	files := migrationFiles(t, ".down.sql")
	slices.Reverse(files)
	d.apply(t, files)
}

func (d *Database) apply(t *testing.T, files []string) {
	t.Helper()
	for _, file := range files {
		statements, err := os.ReadFile(file) //nolint:gosec // the path comes from the repository's own migrations folder
		if err != nil {
			t.Fatalf("read migration %s: %v", filepath.Base(file), err)
		}
		if _, err := d.SQL.ExecContext(t.Context(), string(statements)); err != nil {
			t.Fatalf("apply migration %s: %v", filepath.Base(file), err)
		}
	}
}

func openConnection(t *testing.T, base *mysql.Config, name string) *sql.DB {
	t.Helper()
	config := base.Clone()
	config.DBName = name
	config.MultiStatements = true
	config.ParseTime = true
	config.Loc = time.UTC
	config.Params = map[string]string{"time_zone": "'+00:00'"}
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatalf("open test connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatalf("reach MariaDB through %s: %v", DSNVariable, err)
	}
	return db
}

func migrationFiles(t *testing.T, suffix string) []string {
	t.Helper()
	_, source, _, isKnown := runtime.Caller(0)
	if !isKnown {
		t.Fatal("locate the testdb source file")
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(source), "..", "..", "migrations", "*"+suffix))
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no %s migrations found", suffix)
	}
	slices.Sort(files)
	return files
}
