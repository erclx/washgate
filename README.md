# washgate

A car pulls up to the wash hall. Is it allowed in, what does it cost, and does the wash count exactly once?

washgate answers that for a staffed car wash chain: a camera photo becomes a plate, the site decides admit, deny, or ask staff, and every wash lands in the ledger once, even when the camera fires twice, the payment provider retries, or the site loses its link to head office. It's a working model of the problem, built to show how the backend stays correct when conditions are bad.

## How it works

- **Plate reader** (Python): a pretrained model turns a lane photo into plate text and a confidence score.
- **Site agent** (Go, one per site): decides entry from its own SQLite copy, so a dropped link never stops the lane, then syncs washes to head office through an outbox.
- **Central** (Go): holds customers, subscriptions, and washes in MariaDB and takes payment events from Stripe test mode.
- **Invoicing** (PHP): builds the monthly fleet invoice from the same database.
- **washctl** (Go): the operations CLI for plate lookups, quota fixes, and invoices.
- **Dashboard** (React): shows each lane decision with the backend work behind it, plus a customer app mockup.

Only the camera and the customers are simulated. Every service, database, and network call runs for real.

## Status

Scaffolded, not yet working end to end. Each component builds and passes its checks. The central API stores each wash a site sends exactly once and serves the entitlement changes a site pulls, though no site pushes or pulls yet. The use cases the system is built to handle are in [canon/REQUIREMENTS.md](canon/REQUIREMENTS.md).

## Setup

You need [Docker](https://docs.docker.com/get-docker/), [Bun](https://bun.sh), [Go](https://go.dev/dl/), PHP 8.3 with Composer, and [uv](https://docs.astral.sh/uv/).

```bash
bun install
(cd web && bun install)
(cd invoicing && composer install)
(cd plate-reader && uv sync)
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
```

## Usage

Start MariaDB, apply the migrations, and run the central API, then check it:

```bash
docker compose up --build
curl localhost:8080/health
```

Every component folder takes the same commands:

```bash
bun run lint:fix
bun run typecheck
bun run test:run
```

Run `bun run check` at the root before committing. It formats and checks spelling, shell scripts, and markdown across the repo.

## Repository layout

| Folder          | Holds                                              |
| --------------- | -------------------------------------------------- |
| `core/`         | Go module: central, the site agent, and washctl    |
| `invoicing/`    | PHP invoicing service                              |
| `plate-reader/` | Python plate reader                                |
| `web/`          | React dashboard and customer app mockup            |
| `canon/`        | Requirements, architecture, and per-domain context |

## Working with an agent

An agent loads [CLAUDE.md](CLAUDE.md) first. It points at the requirements, the architecture record, and the context index, and `.claude/rules/` holds the conventions each language follows.

## Support

Open an issue on this repository.

## License

[MIT](LICENSE)
