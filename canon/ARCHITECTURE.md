# Architecture

## Overview

Each wash site runs its own site agent, which decides entry from a local copy of who is allowed in and syncs washes to a central API at head office. A plate reader in front of each lane turns a photo into plate text. Central owns customers, subscriptions, and washes in MariaDB, takes payment events from Stripe, and a PHP service builds the monthly fleet invoices from the same database. A React app shows each lane decision with its backend trace, and carries the customer app mockup.

This record holds at most 12 decisions.

## Key technical decisions

### Go for central, the site agent, and washctl

The three services that decide entry, hold the ledger, and operate the system are one Go module, using the standard library's `net/http` and `database/sql` with no framework or ORM. Go is the advert's primary backend language, and the standard library keeps every file explainable. A framework would add a layer to learn for no behavior the system needs.

### PHP for invoicing, Python for the plate reader

Invoicing is PHP because the advert's backend runs Go and PHP side by side, and a report over the same database is where a second language sits naturally. The plate reader is Python because the model tooling lives there, and because the camera vendor's box sits outside the backend in a real site too. Rewriting either in Go would drop the reason each exists.

### Decide at the site, sync to central

A site agent decides from its own SQLite copy and writes each wash to a local ledger and an outbox in one transaction, then pushes the outbox to central. Deciding at central would stop every lane the moment a link drops. The cost is that two offline sites can each admit the same Premium car past its cap, which the system accepts and reconciles.

### Idempotent writes at every repeatable boundary

Every input that can arrive twice carries an id the receiver stores in the same transaction as its effect: wash ids from the site, event ids from Stripe, and a dedup window on plate reads at the lane. A retry after any crash changes nothing.

### Docker Compose delivery

The whole system, MariaDB included, starts with one `docker compose up` on a developer machine. There is no deployed environment.

### Per-component tooling behind shared script names

Each component folder carries its own `package.json` exposing `lint`, `lint:fix`, `typecheck`, `test`, and `test:run`, backed by that language's tools: golangci-lint and `go test`, PHP-CS-Fixer, PHPStan, and PHPUnit, ruff, mypy, and pytest, ESLint, TypeScript, and Vitest. The root carries the repo-wide format, spelling, shell, and markdown checks and the git hooks. One script name works in every folder, so no one learns four toolchains to verify a change.

### Component folders at the root

`core/`, `invoicing/`, `plate-reader/`, and `web/` sit side by side, one per language, each owning its own dependencies. Go keeps `cmd/<binary>/` for entry points and `internal/<component>/` for logic, with imports flowing from `cmd` inward and never between components. `.claude/rules/project/` holds the Go and PHP rules the toolkit does not ship.

### Uncertain reads go to staff

A plate read is accepted only at or above a confidence cutoff and in a known plate shape. Anything else goes to the staff on the lane. Confidence does not catch a different car's plate read cleanly, so the lookup limits the damage: a misread matching no subscriber falls through to pay-per-wash.

## Risks / open questions

- How a real lane camera frames a car, and how often a second car is in view, is unknown. It decides how much the wrong-plate case matters.
- Whether admitting past the cap while offline is the right business call is a guess to confirm, not a technical fact.
- The Go and PHP rules were written before any real Go or PHP code. Revise them once the first services exist.
