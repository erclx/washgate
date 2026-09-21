# Project

washgate models the entry decision at a staffed car wash: a plate is read at the lane, the site decides admit, pay, or ask staff, and every wash is counted once and billed correctly. A monorepo of Go services, a PHP invoicing service, a Python plate reader, and a React dashboard over MariaDB and SQLite.

## Context

- Before non-trivial work in a domain read `canon/context/<domain>.md`, and before touching a UI surface read `canon/wireframes/<surface>.md`. Pick which from the index anchors below.

@canon/REQUIREMENTS.md
@canon/ARCHITECTURE.md
@canon/context/index.md
@canon/wireframes/index.md

## Commands

- Run `bun run check` at the root before committing. It formats and checks spelling, shell, and markdown across the repo.
- Run `bun run lint:fix`, `bun run typecheck`, and `bun run test:run` inside every component folder the change touched. Each folder carries the same script names.

## Key paths

- `core/`: the Go module, with `cmd/central`, `cmd/site-agent`, and `cmd/washctl` as entry points and `internal/<component>/` for their logic
- `invoicing/`: the PHP service that builds monthly fleet invoices
- `plate-reader/`: the Python service that turns a lane photo into plate text and a confidence score
- `web/`: the React dashboard and the customer app mockup
- `canon/DESIGN.md`: design tokens and the visual system
