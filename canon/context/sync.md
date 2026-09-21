---
title: Sync
description: Site to central contract, entitlement change log, outbox and pull, offline behavior, and migration numbering for central and the site
---

# Sync

## Overview

A site agent decides entry from its own SQLite copy and never asks central mid-decision. Two calls keep the copy and the ledger in step. The site pushes admitted washes to `POST /washes`, and pulls entitlement changes from `GET /entitlements?after=`. Both sit behind a bearer token, and both are safe to repeat.

## Layout

- `core/internal/central/` owns the ledger, the two site endpoints, and the change log in MariaDB
- `core/internal/siteagent/` owns the SQLite copy, the outbox, and the `Syncer` that runs both calls
- `core/migrations/` owns central's schema as numbered up and down pairs
- `core/internal/siteagent/schema/` owns the site's schema, embedded in the binary
- `core/offlinesync/` owns the test that runs central and a site through a link that can drop

## Decisions

### Wash push

- Central stores a wash under the id the site minted, so a replayed batch, or an outbox rebuilt after a crash, converges. `POST /washes` answers which ids it stored and which it already held, and the site clears the outbox only for ids named in either list.
- A duplicate wash id is forgiven with `ON DUPLICATE KEY UPDATE id = id`. `INSERT IGNORE` would also turn an unknown company into a warning and acknowledge a wash central never stored.
- A batch is one transaction. An unknown site or company rolls the whole batch back and answers 422.
- A failed push does not skip the pull. A batch central refuses as a whole stays at the head of the outbox and holds back later washes until someone repairs it, though no wash a site can write is refused that way today.
- The site holds no SQLite transaction across an HTTP call. It runs on one connection, so an open transaction would stall every lane for the length of a push timeout.

### Change log

- Every write that changes a plate's plan, its company, or its quota reset appends a row to `entitlement_changes` in the same transaction. The writer first takes the single `entitlement_cursor` row with `FOR UPDATE`.
- The lock exists because an auto-increment `seq` can be handed out before an earlier transaction commits. A site pulling in that gap would move past a change it never saw. With the lock, changes commit in `seq` order.
- Every writer takes the cursor before any subscription row, so two writers cannot deadlock.
- A write to `vehicles` that changes none of the three appends nothing, because a site holds only Premium and fleet plates and their resets.
- A quota reset appends a change that keeps the plan `premium` and carries `quota_reset_at`. It is the one change that alters neither plan nor company. The site writes the instant to its `quota_resets` table, kept apart from `vehicles` so a later plan rewrite cannot erase it, and only ever moves it forward. A change replayed out of order cannot undo a later reset.
- A paid renewal appends a change that alters nothing, carrying the same plan. The site applies every change as an upsert, so the repeat is harmless, and central needs no rule to tell a first payment from a renewal.
- A change with no plan revokes the plate, and the site deletes the vehicle. A fleet change carries the company name, which the site writes as an insert or update, so it never needs a second call to show it.
- The site applies a whole page and moves its cursor and pull time in one transaction, so a change it cannot apply leaves the copy as it was.

### Offline

- Offline is measured as time since the last good pull, not by a failed ping. A site that can push but not pull is still deciding from an aging copy.
- Past `SITE_MAX_OFFLINE`, which defaults to 10 minutes, an unknown plate goes to staff as `unknown_plate_offline` instead of to payment. A stale copy cannot say the plate holds no subscription.
- A copy that has never pulled counts as stale, so a new site's first minutes send unknown plates to staff. Known plates are unaffected.
- Two sites offline at once can each admit the same Premium car past its cap of eight, since each holds only its own washes until the link returns. The system accepts this and reconciles at central. `canon/ARCHITECTURE.md` records the reason.

## Hidden contracts

- `CENTRAL_SITE_TOKENS` is a comma-separated list of `site-id=token` pairs and is the whole set of sites that may authenticate. Central stores only the SHA-256 of each token.
- Central clears every stored hash before it writes the list. A site dropped from the list keeps its row and loses its access at the next restart, and an empty value revokes every site.
- A token under 32 characters, a repeated site, or a token given to two sites stops central at start.
- A site pushes only under its own id. A batch naming another site answers 403 before the store is touched.
- A wash carries `plan` of `premium` or `fleet`, and only a fleet wash names its company. Central's request validator enforces the pairing before the store is touched.
- A pull page holds up to 500 changes by default and 1000 at most. The site asks for 500 and keeps pulling until a page comes back short.

## Migrations

Central and the site number their schema differently, and the difference bites when two branches each add one.

- Central files sit in `core/migrations/` as `NNNN_name.up.sql` and `.down.sql`, four digits. `scripts/migrate.sh` records each applied version by name in `schema_migrations` and applies any file not yet recorded.
- A lower-numbered central file that merges later still runs on the next `docker compose up`. `down` rolls back the highest recorded version, and every `CREATE` is `IF NOT EXISTS`, so a rerun is safe.
- Site files sit in `core/internal/siteagent/schema/` as `NNN_name.up.sql`, three digits. The site reads `PRAGMA user_version` and applies each file above it, in order, one transaction each, then sets the version to that file's number. A lower-numbered file merged after a higher one never runs on a database already past it.
- Whichever branch merges second renumbers its own pair when the numbers collide.

## Gotchas

- Central's `docker compose` service starts only after `migrate` completes. The site agent does not wait for central. A site that starts first is the offline case, and it must keep deciding.
- `core/internal/siteagent/store.go` sets one open connection on SQLite. That is what serializes two reads of one plate, so do not raise it.
- Tests that touch central skip when `CENTRAL_TEST_DSN` is unset, and name the variable. CI sets it against a `mariadb:11.8` service. A green local run with it unset proved nothing about central.

## Adding a change type

A new write that alters a plate's plan, company, or quota reset goes through `lockEntitlementCursor` and `appendEntitlementChange` in `core/internal/central/store.go`, inside the transaction that makes the write. `ResetQuota` in `core/internal/central/operator_store.go` is the example for a change that carries a new field. Add a site-side case in `applyChange` in `core/internal/siteagent/store.go` only if the change shape is new, and a column on `entitlement_changes` in a new central migration.
