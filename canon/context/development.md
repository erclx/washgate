---
title: Development
description: Local dev workflow, scripts, and husky hooks
---

# Development

## Overview

Owns how the project runs on a developer machine: installing the toolchain, the scripts that verify a change, and the git hooks that run them before a commit or a push leaves. CI calls the same scripts from a workflow, which is the CI entry's subject.

## Layout

- `scripts/` owns the shell scripts the package scripts below call
- `.husky/` owns the git hooks
- `core/`, `invoicing/`, `plate-reader/`, and `web/` each carry their own `package.json` with the same verify script names

## Setup

- Install [Bun](https://bun.sh): `curl -fsSL https://bun.sh/install | bash`
- Install [Go](https://go.dev/dl/) and put its `bin` on `PATH`, then install the linter: `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`
- Install PHP 8.3 and Composer: `sudo apt install -y php-cli php-mysql php-xml php-mbstring composer`
- Install [uv](https://docs.astral.sh/uv/) for the plate reader
- Install dependencies: `bun install` at the root and in `web/`, `composer install` in `invoicing/`, `uv sync` in `plate-reader/`

## Running the stack

- `docker compose up --build` starts MariaDB, runs the `migrate` step, then starts central once the step exits cleanly. Use `--build` so a changed central image is never served from a stale layer.
- `core/migrations/` holds numbered `.up.sql` and `.down.sql` pairs. `scripts/migrate.sh` applies each unrecorded `.up.sql` in name order and records it in `schema_migrations`.
- `docker compose run --rm migrate down` rolls back the latest recorded migration.
- Connection settings are the `DB_*` variables in `.env.example`. The compose file falls back to the same defaults when a variable is unset.
- MariaDB commits DDL implicitly, so a migration failing halfway leaves a partial schema. Keep one statement per file or use `IF NOT EXISTS`.
- Central registers each site named in `CENTRAL_SITE_TOKENS` at startup, as comma-separated `site-id=token` pairs with each token at least 32 characters, and exits on a malformed value. Compose sets `site-1` from `SITE_TOKEN`, whose default in `.env.example` is a local development value rather than a secret.
- `POST /washes` and `GET /entitlements` answer 401 without a site's token, so a manual request to either sends `Authorization: Bearer <token>`. `GET /health` stays open.
- Data lives in the `mariadb-data` volume. `docker compose down -v` is the one command that drops it.
- The central MariaDB tests in `core/` read `CENTRAL_TEST_DSN` and skip when it is unset, so `bun run test:run` stays green without compose. To run them, bring up compose's MariaDB and point the variable at a user that can create databases, such as root: `CENTRAL_TEST_DSN='root:washgate-root@tcp(127.0.0.1:3306)/'`. The compose `washgate` user cannot create databases.
- `core/internal/testdb` gives each test its own database with every migration applied and drops it afterwards.

## Scripts

| Command          | Purpose                                                                        |
| ---------------- | ------------------------------------------------------------------------------ |
| `bun run check`  | Repairs locally. Auto-formats, then asserts what the formatters could not fix. |
| `bun run format` | Auto-fix prettier and shfmt formatting.                                        |

Every component folder carries these, backed by its own language's tools:

| Command             | Purpose                                         |
| ------------------- | ----------------------------------------------- |
| `bun run lint`      | Report lint and format findings without fixing. |
| `bun run lint:fix`  | Fix lint and format findings.                   |
| `bun run typecheck` | Static analysis: `go vet`, PHPStan, mypy, tsc.  |
| `bun run test:run`  | Run the unit tests verbosely.                   |

## Shell scripts

Every `.sh` file lives under a `scripts/` folder, either the root's or the component's it serves (`web/scripts/`, `plate-reader/scripts/`). Do not place a shell script anywhere else.

## Husky hooks

- `pre-commit` runs `lint-staged` (prettier, cspell, shfmt, shellcheck on staged files).
- `commit-msg` runs `commitlint` against the conventional commit format.
- `pre-push` runs `bun run check`. When a markdown-bans audit tool is on PATH, it also gates on banned characters, words, and spellings across every tracked markdown file except `CHANGELOG.md`. After pushing, run `git status`. If files changed, commit the diff as `style(<scope>):` and push again.
