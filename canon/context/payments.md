---
title: Payments
description: Premium subscriptions through Stripe test mode, checkout, webhook signature and idempotency, handled events, and the off state
---

# Payments

## Overview

Premium is the only plan a customer buys, and it is bought only through Stripe in test mode. Central starts a Checkout session, Stripe sends signed events back, and each event starts, extends, or ends a subscription exactly once. Every change reaches the lanes through the entitlement log described in `canon/context/sync.md`. No real money moves.

## Layout

- `core/internal/central/stripe/` owns the Stripe client, signature check, and event decoding, and imports nothing from central
- `core/internal/central/` owns the two routes, the price read, and the subscription writes in the store
- `core/migrations/` owns the `stripe_events` table and the `prices` table that holds the Premium price

## Flow

1. `POST /checkout` takes a `customer_id` and a `plate`, and answers 201 with a `checkout_url` to send the customer to.
2. The customer pays on Stripe's page.
3. Stripe posts `invoice.paid` to `POST /stripe/webhook`. The first paid invoice starts the subscription and each later one extends it.
4. Stripe posts `customer.subscription.deleted` when the subscription ends, and central cancels it and revokes the plate.

`POST /checkout` answers 400 for a body it cannot use, 409 when the plate is registered to another customer or a fleet company, 422 for an unknown customer, 502 when Stripe fails, and 503 when there is no Premium price or Stripe is off. The price is read from the row of `prices` valid now, in öre, and sent to Stripe inline in SEK for a monthly subscription. The customer and plate travel as subscription metadata so the paid invoice names them.

## Decisions

- The subscription starts from `invoice.paid` alone. Stripe does not order checkout and invoice events, so acting on the invoice means the order never matters.
- The webhook verifies the signature over the raw body before decoding it. The check is a SHA-256 signature made with the webhook secret over the timestamp and payload, accepts any `v1` signature in the header, and refuses a timestamp more than 300 seconds from now as a replay.
- The event id goes into `stripe_events` in the same transaction as its effect. A repeated event id changes nothing and answers 200, so Stripe's retries and a manual resend both charge and extend once. A store failure answers 500 so Stripe retries.
- An event of a type central does not handle still stores its id and answers 200, so Stripe stops resending it.
- A renewal extends `current_period_end` with `GREATEST`, so a late event never shortens a period.
- A paid invoice for a subscription that is no longer active is ignored, not revived. Stripe never revives a deleted subscription, and a local cancel or an operator's replacing write leaves the older Stripe subscription dead.
- Starting a subscription cancels any other active one on the plate and registers the vehicle to the customer, in the same transaction.
- Checkout refuses a plate held by another customer or a company. Otherwise the paid invoice would take that vehicle over.
- Checkout drops a taxi's trailing `T` with the same rule as the lane, so `ABC123T` is sold as `ABC123`. See `canon/context/plates.md`.
- The Stripe API version is pinned to `2025-03-31.basil` in `core/internal/central/stripe/client.go`, and event payloads are decoded in that shape. Keep it equal to the account's default version, since events the Stripe CLI forwards use the default. A payload missing a field central needs answers 400 and usually means a version mismatch.

## Gotchas

- A live key never starts. `NewClient` accepts only `sk_test_` and `rk_test_` prefixes, and central exits at start on anything else.
- With `STRIPE_SECRET_KEY` or `STRIPE_WEBHOOK_SECRET` unset, central starts with Stripe off and both routes answer 503. The stack runs with no Stripe account.
- The `stripe-cli` Compose service forwards test events to `central:8080/stripe/webhook` and runs only under `docker compose --profile stripe up`. It needs the same test key.
- The event fixtures under `core/internal/central/stripe/` follow the documented shape of the pinned version. They were not captured from a real account, so a real event from the account is the check that they match.
- Nothing in central seeds a customer or a Premium price. No route creates a customer and no migration inserts a `prices` row, so checkout answers 422 for any customer and 503 for lack of a price until both rows are inserted by hand.
- The success and cancel URLs come from `CHECKOUT_SUCCESS_URL` and `CHECKOUT_CANCEL_URL` and default to `http://localhost:5173/`.

## Adding a handled event

Add its type and decoded shape in `core/internal/central/stripe/event.go`, map it to a `SubscriptionAction` in `subscriptionEvent` in `core/internal/central/webhook.go`, and apply it in `ApplySubscriptionEvent` in `core/internal/central/store.go`. Take the cursor lock first, and append an entitlement change when the plate's plan or company changes.
