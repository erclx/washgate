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
- Pushes to `main` that touch `web/` or `deploy.yml`, for the deploy workflow only

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
| Web E2E      | `web/`          | `playwright install --with-deps`, then `test:e2e`        |

The core job runs a `mariadb:11.8` service with a health check and sets `CENTRAL_TEST_DSN` to its root user, so the central integration tests run on every pull request instead of skipping. The invoicing job runs the same service and sets `INVOICING_TEST_DSN`, `INVOICING_TEST_USER`, and `INVOICING_TEST_PASSWORD` to its root user.

`.github/workflows/deploy.yml` publishes the replay build to Cloudflare Pages on a push to `main` or a manual run:

| Job        | Runs                                                                                                 |
| ---------- | ---------------------------------------------------------------------------------------------------- |
| Web Checks | `typecheck`, `lint`, `test:run` in `web/`                                                            |
| Replay     | `build:replay` in `web/`, copies the photo attribution into `dist`, uploads `web/dist`               |
| Deploy     | `wrangler pages deploy` to the `washgate` project, skipped with a notice while the secrets are unset |

`.github/workflows/phase-label-gate.yml` scans a pull request for a phase label, a board identifier, or a session link.

## Running CI locally

`bun run check` at the root runs the repo-wide asserts and auto-formats first. Run `bun run lint`, `bun run typecheck`, and `bun run test:run` inside each component folder for its job. If CI fails on format, run `bun run check` locally and commit the diff.

## Decisions

- Component jobs live in `components.yml` rather than in `verify.yml`, because `verify.yml` is a golden file a canon sync overwrites.
- End to end tests for `web/` run in their own job rather than as steps of the web job. `test:e2e` builds the Replay bundle and serves it, so the job needs no backend, and it caches browsers keyed on the Playwright version. The compose spec skips there, since CI starts no stack.
- The deploy uploads a prebuilt bundle, so Cloudflare runs no build and `web/dist` is all it serves. A run from a branch other than `main` lands on a preview host, and a closed pull request deletes its preview deployments.
- The deploy job skips with a notice while `CLOUDFLARE_API_TOKEN` or `CLOUDFLARE_ACCOUNT_ID` is unset, so a merge before the operator finishes setup leaves no red run on `main`.
