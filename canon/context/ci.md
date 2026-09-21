---
title: CI
description: GitHub Actions workflow triggers and checks
---

# CI

## Overview

Owns the GitHub Actions workflows that gate a merge: which events start a run, and which checks have to pass before the branch can land. The checks call package scripts rather than defining commands of their own, so what each one runs is the development entry's subject.

## Layout

- `.github/workflows/` owns every workflow. GitHub reads no other `.github/` folder, so a component never carries its own.

## Triggers

- Pull requests targeting `main`
- `workflow_dispatch` (manual run from the Actions tab)

## Checks

All jobs across the workflows below must pass before merge.

`.github/workflows/verify.yml` runs the repo-wide checks from the root:

| Check  | Command                | What it asserts                    |
| ------ | ---------------------- | ---------------------------------- |
| Format | `bun run check:format` | prettier and shfmt are clean       |
| Spell  | `bun run check:spell`  | cspell passes against dictionaries |
| Shell  | `bun run check:shell`  | shellcheck passes at warning level |

`.github/workflows/components.yml` runs one job per component, each inside its own folder:

| Job          | Folder          | Runs                                                     |
| ------------ | --------------- | -------------------------------------------------------- |
| Core (Go)    | `core/`         | `lint`, `typecheck`, `go test -race ./...`, `build`      |
| Invoicing    | `invoicing/`    | `composer install`, then `lint`, `typecheck`, `test:run` |
| Plate reader | `plate-reader/` | `uv sync --frozen`, then `lint`, `typecheck`, `test:run` |
| Web          | `web/`          | `typecheck`, `lint`, `test:coverage`, `build`            |

The core job runs a `mariadb:11.8` service with a health check and sets `CENTRAL_TEST_DSN` to its root user, so the central integration tests run on every pull request instead of skipping.

`.github/workflows/phase-label-gate.yml` scans a pull request for a phase label, a board identifier, or a session link.

## Running CI locally

`bun run check` at the root runs the repo-wide asserts and auto-formats first. Run `bun run lint`, `bun run typecheck`, and `bun run test:run` inside each component folder for its job. If CI fails on format, run `bun run check` locally and commit the diff.

## Decisions

- Component jobs live in `components.yml` rather than in `verify.yml`, because `verify.yml` is a golden file a canon sync overwrites.
- End to end tests for `web/` are not in CI yet. Add a job once the first real spec exists.
