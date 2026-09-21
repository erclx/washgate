---
description: Enforce Go test placement, table-driven cases, and httptest patterns in core/
paths:
  - 'core/**/*_test.go'
---

# Go testing tooling

## Layer

- Load the `canon:test-craft` skill to pick the layer a test belongs at before choosing the tooling below, and report it rather than proceeding silently when the skill does not resolve.

## Framework

- Use the standard `testing` package. Do not add an assertion library.
- Place `<file>_test.go` beside the file it tests, in the same package.
- Name tests `Test<Unit><Behavior>` so the name reads as the assertion (`TestIngestIgnoresReplayedWash`).

## Table-driven cases

- Write cases as a slice of structs with a `name` field and run each with `t.Run(tc.name, ...)`.
- Keep branching out of the loop body. A case needing different steps is its own test.

## Helpers

- Mark a test helper with `t.Helper()` as its first line.
- Use `t.Context()` for request and database contexts, and `t.TempDir()` for files.
- Use `t.Cleanup` over `defer` for teardown a helper registers.

## HTTP and databases

- Drive handlers through `httptest.NewRecorder` and `httptest.NewRequestWithContext`.
- Stand up outbound dependencies with `httptest.NewServer`. Do not call real network services.
- Run database tests against a real MariaDB or SQLite instance. Do not mock `database/sql`.

## Assertions

- Report failures with `t.Fatalf` when the rest of the test cannot run, and `t.Errorf` otherwise.
- Write failure messages as `got X, want Y`.
- Compare structs with `reflect.DeepEqual` or a field-by-field check, never by formatted string.

## Running

- Run the race detector on concurrent code with `go test -race ./...`.
