---
title: Lane
description: Lane decision of admit, pay, or staff with every reason code and its check order, the dedup window, Premium cap, fleet and prepaid washes, and the no-stored-image rule
---

# Lane

## Overview

The site agent answers one question per plate read: admit the car, send it to pay, or ask staff, with a reason the staff can read. It answers from its local SQLite copy alone, so a lane keeps deciding when the link to central is down. The plate reader feeds it, and `canon/context/sync.md` covers how the copy and the ledger stay in step. Central's operator routes for plate lookup, quota resets, and the site list, and the site's `GET /status`, sit beside the lane and are described under Operator surface.

## Layout

- `core/internal/siteagent/decide.go` owns the decision as one pure function over a read, the facts about its plate, and the policy
- `core/internal/siteagent/router.go` owns `POST /reads`, which gathers the facts, decides, and records an admitted wash
- `core/internal/siteagent/store.go` owns the facts queries and the wash ledger the decision counts against
- `core/cmd/site-agent/main.go` owns the environment variables that set the policy
- `core/internal/central/` owns the operator routes, which read and correct what the lanes hold

## Decision order

`Decide` checks in this order and stops at the first match. The order is the contract, since a read can match several.

1. Confidence below the cutoff: `staff`, `low_confidence`
2. A plate that matches no accepted shape: `staff`, `malformed_plate`
3. The plate's latest wash is inside the dedup window: `admit`, `duplicate_read`, carrying that wash's id and recording nothing
4. A fleet plate: `admit`, `fleet`
5. A Premium plate with fewer than eight washes counted this month: `admit`, `within_cap`
6. A plate holding an unspent prepaid wash: `admit`, `prepaid_wash`, spending the oldest one
7. A Premium plate at eight counted washes: `pay`, `cap_reached`
8. No entitlement and a copy that has never pulled or last pulled longer ago than the limit: `staff`, `unknown_plate_offline`
9. No entitlement otherwise: `pay`, `unknown_plate`

The router normalizes the plate before it looks the plate up. See `canon/context/plates.md` for the rule, the cutoff, and why a bad read never reaches the lookup.

## Decisions

- Dedup runs ahead of the cap. A camera that fires twice on a car's eighth wash sees the second read admitted as a duplicate instead of billed as the ninth.
- The dedup window is `SITE_DEDUP_WINDOW`, default 120 seconds. It looks at the plate's latest wash, so it covers a car that was admitted and never a car sent to pay.
- The router records the wash in one transaction that repeats the dedup check. Two reads of one plate racing each other insert one wash, and the loser answers `duplicate_read` with the winner's id. The site's single SQLite connection is what makes the check hold.
- Only an admit writes a wash. A `pay` or `staff` decision writes nothing, and the lane charges nothing after `pay`. A single wash is bought ahead of time through `POST /checkout/single-wash` and reaches the lane as a prepaid wash, which `canon/context/payments.md` covers.
- A prepaid wash comes after fleet and Premium within the cap, and ahead of `cap_reached` and both unknown-plate branches. An included wash goes first, so a paid wash is never spent on a car the plan admits anyway, and a driver who paid is admitted even on a stale copy.
- An admit on a prepaid wash writes a wash with plan `prepaid` naming the prepaid wash, and marks it spent in the same transaction. A duplicate read inside the window spends nothing, since the dedup check returns before the spend.
- The Premium cap is eight washes per calendar month, counted in the site's time zone, `SITE_TIME_ZONE`, default `Europe/Stockholm`. The month starts at midnight local on the first, so the cap resets at Stockholm midnight and not at UTC midnight. The binary embeds the time zone database so a slim image can load the zone.
- The count holds only the plate's `premium` washes, so a fleet or prepaid wash never uses up an included one. It starts at the later of the month start and the plate's latest quota reset, so a reset restarts the cap mid-month. `Facts` in `core/internal/siteagent/store.go` reads it. The reset instant reaches the site through the change log in `canon/context/sync.md`.
- The count is the site's own ledger for the plate. It holds this site's washes only, so two sites can each admit up to the cap, which `canon/context/sync.md` records as accepted.
- A fleet wash is written with its company and is pushed to central against that company. A fleet plate has no cap and no monthly count.
- A wash stores the plan the plate held when it was admitted, so a later change to the entitlement leaves earlier washes as they were.

## Hidden contracts

- `POST /reads` takes JSON with a `plate` string and a `confidence` from 0 to 1, both required. A missing field, a confidence outside the range, a second JSON value, or a body that is not JSON answers 400, and a content type other than `application/json` answers 415.
- The answer is `decision`, `reason`, and, when a wash exists, `wash_id`. Extra fields in the request are ignored, which is why the reader's `box` does not break it.
- The decision log line carries the decision, reason, and wash id and never the plate.
- No plate image is written to disk or logged anywhere in the stack. The reader decodes the photo in memory and posts it on, and the site agent reads no photo field.

## Gotchas

- The site agent caps `POST /reads` at 4 KiB. The plate reader sends the photo as base64 beside the read, which is far larger, so the site agent refuses that body with 400 and the reader logs the failed send. The reader's own answer is unaffected. Until the cap or the feed changes, a read reaches the lane from a caller that posts `plate` and `confidence` alone.
- `unknown_plate_offline` fires on a brand new site before its first pull, since a copy that never pulled counts as stale. Known plates still admit.
- `cap_reached` is a `pay` outcome, not a refusal. The staff copy offers a single wash instead.

## Operator surface

Head office reads and corrects what the lanes hold through routes that sit beside the lane and never decide entry.

- `GET /plates/{plate}` on central answers a plate's owner, subscription, this month's washes, its Premium washes since its latest reset, and the reset instant. A malformed plate answers 400 and a plate central holds no vehicle for answers 404.
- `POST /plates/{plate}/quota-resets` on central takes a reset `id` and an optional note. It applies only to a plate on an active Premium subscription, answering 404 with none and 409 for any other plan. The id is stored with its effect, so a retry returns the stored reset with 200, and an id already used for another plate answers 409.
- `GET /sites` on central lists each site with its last push time.
- `GET /status` on the site agent answers its site id, its outbox depth, and when its copy last pulled. Central cannot know either number.

Central's operator routes carry no site token, and a site token never opens them. Central binds to loopback in Compose and that is the only guard. All three take a plate in any typed form and read it through `canonicalPlate`, the full rule from `canon/context/plates.md`.

Central counts a lookup's month in a fixed `Europe/Stockholm`, while each site counts its cap in its own `SITE_TIME_ZONE`. The two agree at the default and drift apart only if a site changes its zone.

## Adding a reason

Add the constant and its branch in `Decide` in `core/internal/siteagent/decide.go` at the place the order above needs, with a case in `decide_test.go`. The dashboard's staff copy reads the reason string, so a new reason needs copy in `canon/wireframes/lane-dashboard.md` before it ships.
