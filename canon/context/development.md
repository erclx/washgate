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
- `core/`, `invoicing/`, `plate-reader/`, and `web/` each carry their own `package.json` with the same verify script names, and their own `compose.yaml` holding that component's services
- `docker-compose.yml` at the root pulls the four component files in with `include:` and keeps `mariadb`, `migrate`, and the `mariadb-data` volume, which no component owns

## Setup

- Install [Bun](https://bun.sh): `curl -fsSL https://bun.sh/install | bash`
- Install [Go](https://go.dev/dl/) and put its `bin` on `PATH`, then install the linter: `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`
- Install PHP 8.3 and Composer: `sudo apt install -y php-cli php-mysql php-xml php-mbstring composer`
- Install [uv](https://docs.astral.sh/uv/) for the plate reader
- Install dependencies: `bun install` at the root and in `web/`, `composer install` in `invoicing/`, `uv sync` in `plate-reader/`

## Running the stack

A branch writes only its own component's entries: its `compose.yaml`, its section of `.env.example`, its bullet in the README's Status, and its subsection below. Only a change to the database or the migrate step edits the root `docker-compose.yml` or the Stack subsection. Compose 2.20 or newer is needed for `include:`. A relative path in a component's `compose.yaml` resolves against that file's folder, and a variable read by two components, such as `DB_*` or `SITE_TOKEN`, keeps its own `${VAR:-default}` in each.

### Stack

- `docker compose up --build` starts MariaDB, runs the `migrate` step, then starts central once the step exits cleanly. Use `--build` so a changed central image is never served from a stale layer.
- `core/migrations/` holds numbered `.up.sql` and `.down.sql` pairs. `scripts/migrate.sh` applies each unrecorded `.up.sql` in name order and records it in `schema_migrations`.
- `docker compose run --rm migrate down` rolls back the latest recorded migration.
- Connection settings are the `DB_*` variables in `.env.example`. The compose files fall back to the same defaults when a variable is unset.
- MariaDB commits DDL implicitly, so a migration failing halfway leaves a partial schema. Keep one statement per file or use `IF NOT EXISTS`.
- Data lives in the `mariadb-data` volume and the site agent's `site-data` volume. `docker compose down -v` is the one command that drops them.

### Central

- Central registers each site named in `CENTRAL_SITE_TOKENS` at startup, as comma-separated `site-id=token` pairs with each token at least 32 characters, and exits on a malformed value. The variable is the whole set: a site left out keeps its row but loses its token, so an empty or unset value revokes every site. Compose sets `site-1` from `SITE_TOKEN`, whose default in `.env.example` is a local development value rather than a secret.
- `POST /washes` and `GET /entitlements` answer 401 without a site's token, so a manual request to either sends `Authorization: Bearer <token>`. `GET /health` stays open.
- The operator routes, `GET /plates/{plate}`, `POST /plates/{plate}/quota-resets` with `{"id": "...", "note": "..."}`, and `GET /sites`, take no token at all, since a site token must never open them and central binds to loopback. A quota reset makes every site count a Premium plate's monthly cap from the reset time once its next pull lands, and a repeated reset id changes nothing.
- The central MariaDB tests in `core/` read `CENTRAL_TEST_DSN` and skip when it is unset, so `bun run test:run` stays green without compose. To run them, bring up compose's MariaDB and point the variable at a user that can create databases, such as root: `CENTRAL_TEST_DSN='root:washgate-root@tcp(127.0.0.1:3306)/'`. The compose `washgate` user cannot create databases.
- Parallel worktree sessions each start their own throwaway MariaDB for these tests rather than sharing compose's 3306: `docker run -d --name washgate-<topic>-test -e MARIADB_ROOT_PASSWORD=washgate-root -p 127.0.0.1:<free port>:3306 mariadb:11.8`, with `CENTRAL_TEST_DSN` pointed at that port.
- `core/internal/testdb` gives each test its own database with every migration applied and drops it afterwards.
- Central's Stripe routes, `POST /checkout` and `POST /stripe/webhook`, answer 503 until `STRIPE_SECRET_KEY` and `STRIPE_WEBHOOK_SECRET` are both set, so the stack starts with no Stripe account. Central exits on a key that is not `sk_test_` or `rk_test_`. `docker compose --profile stripe up` adds the `stripe-cli` forwarder.

### Site agent

- The `site-agent` service runs `site-1` on `SITE_PORT` (default 8081). It has no `depends_on` central, since a site starting before central is the offline case. Every `SITE_SYNC_INTERVAL` (default `5s`) it pushes its outbox to central and pulls entitlement changes into its SQLite copy, which lives in the `site-data` volume at `/data/site.db`. Once the last good pull is older than `SITE_MAX_OFFLINE` (default `10m`), or before the first one, an unknown plate goes to staff instead of to payment.
- Cut the site's link with `docker compose stop central`. The site keeps deciding, admitted washes wait in its outbox, and `docker compose start central` lets the next sync push them. The site logs only when the link goes down or comes back up.
- `GET /status` on the site agent answers `site_id`, `outbox_depth`, and `last_synced_at`, the time of the last good pull, so a backlog shows while central is out of reach.

### washctl

- `washctl` calls central, invoicing, and the site agents over HTTP and touches no database. Run it with `go run ./cmd/washctl <command>` from `core/`. It writes results to stdout and problems to stderr, and exits 1 when a call fails and 2 on a usage error.
- `WASHCTL_CENTRAL_URL` (default `http://127.0.0.1:8080`), `WASHCTL_INVOICING_URL` (default `http://127.0.0.1:8082`), and `WASHCTL_SITE_URLS` as `site-1=http://127.0.0.1:8081,site-2=http://...` (default is `site-1` alone) say where each service answers. A malformed value stops the command before any call.
- `washctl plate [--json] <plate>` shows the owner, plan, leasing company, and washes this month with the count since the last reset.
- `washctl quota-reset [--note text] <plate>` records a reset under a generated id, retried with the same id, so a lost answer changes nothing. A site applies it at its next pull.
- `washctl invoice [--split leasing] [-o file] <YYYY-MM>` writes the month's CSV to stdout or the file, and writes nothing when invoicing refuses.
- `washctl health [--json]` prints one row per site central knows, with central's last sync, the site's outbox depth, and its last pull. A site that does not answer, or has no URL configured, shows that instead of failing the command.
- `core/operatorflow/` runs the CLI against central's router over MariaDB and skips without `CENTRAL_TEST_DSN`.

### Invoicing

- The invoicing MariaDB tests read `INVOICING_TEST_DSN`, `INVOICING_TEST_USER`, and `INVOICING_TEST_PASSWORD` and skip when the DSN is unset. Copy the three from `.env.example`. `invoicing/tests/Database/TemporaryDatabase.php` applies `core/migrations/*.up.sql` to a per-test database, so a schema change in Go reaches the PHP tests.
- The `invoicing` compose service serves `GET /invoices/<YYYY-MM>.csv`, with `?split=leasing` adding the leasing company column, on `127.0.0.1:${INVOICING_PORT:-8082}`. It answers 422 while no `fleet_wash` price is in force for a fleet wash.

### Plate reader

- The `plate-reader` service builds from `plate-reader/`, downloads its model weights at image build, and listens on `127.0.0.1:${PLATE_READER_PORT:-8000}`. Drive one read with `curl -H 'Content-Type: image/jpeg' --data-binary @lane.jpg http://localhost:8000/read`. It sends each read and its photo on to `http://site-agent:8081/reads` and still answers when that service is absent. The site agent refuses the photo until the lane feed lands, which the reader logs.

### Dashboard

- The dashboard builds two ways from one codebase, picked by `VITE_DATA_SOURCE` at build time. `live` reads the site agents and central. `replay` plays the recorded run in `web/src/data-source/replay/fixtures/` and makes no network call. `web/vite.config.ts` validates the variable and hands it to the code as the literal `__IS_REPLAY_BUILD__`, since Rollup drops the unused data source only when the branch sits on a literal. A constant imported from another module keeps both bundles whole.
- The compose `web` service serves the Live build on `127.0.0.1:${WEB_PORT:-8090}`. Its nginx proxies `/api/central/` to central and `/api/sites/site-1/` to the `site-agent` service, so the browser stays same-origin and no Go service needs CORS.
- `bun run dev` in `web/` serves Live with the same proxy: `/api/central` to `CENTRAL_URL` (default `http://localhost:8080`), and each site in `VITE_SITES` to a site agent counting up from port 8081, so a second site would land on 8082, which invoicing holds. `VITE_SITES` is a comma-separated `id=name` list and defaults to `site-1=Site 1`.
- `bun run build:replay` builds Replay into `web/dist/`, and `bun run preview` serves it. `VITE_BASE_URL` sets the base path when the build is hosted under a subpath.
- The design spacing steps are Tailwind v4 theme tokens named `xs` to `xl`, so a sizing utility sharing a name, such as `max-w-sm`, resolves to the spacing value (8px) rather than the container width. Size widths with an arbitrary value such as `max-w-[24rem]`.
- `bun run test:e2e` builds Replay, serves it, and runs the Playwright specs against it. The compose spec `web/e2e/lane-decision.spec.ts` skips unless `WASHGATE_STACK_URL` points at the running `web` service, such as `http://localhost:8090`, and it posts the plate reader's fixture photo to `PLATE_READER_URL` (default `http://localhost:8000`).

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
