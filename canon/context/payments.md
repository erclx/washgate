---
title: Payments
description: Premium subscriptions and prepaid single washes through Stripe test mode, checkout, webhook signature and idempotency, handled events, and the off state
---

# Payments

## Overview

A customer buys one of two things, a Premium subscription or a single wash, and buys either only through Stripe in test mode. Central starts a Checkout session, Stripe sends signed events back, and each event starts, extends, or ends a subscription, or grants one prepaid wash, exactly once. Every change reaches the lanes through the entitlement log described in `canon/context/sync.md`. No real money moves.

## Layout

- `core/internal/central/stripe/` owns the Stripe client, signature check, and event decoding, and imports nothing from central
- `core/internal/central/` owns the three routes, the price reads, and the subscription and prepaid wash writes in the store
- `core/migrations/` owns the `stripe_events` table, the `prices` table that holds the `premium` and `single_wash` prices, and the `prepaid_washes` table

## Flow

1. `POST /checkout` takes a `customer_id` and a `plate`, and answers 201 with a `checkout_url` to send the customer to.
2. The customer pays on Stripe's page.
3. Stripe posts `invoice.paid` to `POST /stripe/webhook`. The first paid invoice starts the subscription and each later one extends it.
4. Stripe posts `customer.subscription.deleted` when the subscription ends, and central cancels it and revokes the plate.

`POST /checkout` answers 400 for a body it cannot use, 409 when the plate is registered to another customer or a fleet company, 422 for an unknown customer, 502 when Stripe fails, and 503 when there is no Premium price or Stripe is off. The price is read from the row of `prices` valid now, in öre, and sent to Stripe inline in SEK for a monthly subscription. The customer and plate travel as subscription metadata so the paid invoice names them.

A single wash takes a shorter path:

1. `POST /checkout/single-wash` takes the same body and answers 201 with a `checkout_url`. It opens a one-off `mode=payment` session priced from the `single_wash` row, and carries `kind=single_wash`, the customer, and the plate as session metadata.
2. Stripe posts `checkout.session.completed` once the customer pays. Central grants one prepaid wash keyed on the Checkout session id and appends a `prepaid_granted` change, which every site pulls.
3. The first site to admit the car spends it. See `canon/context/lane.md` for where it sits in the decision order.

`POST /checkout/single-wash` answers as `POST /checkout` does, except that a plate registered to another customer or a fleet company answers 422 rather than 409, since a fleet car is admitted already and billed on the invoice. It answers 503 when no `single_wash` price is in force.

## Decisions

- The subscription starts from `invoice.paid` alone. Stripe does not order checkout and invoice events, so acting on the invoice means the order never matters.
- The webhook verifies the signature over the raw body before decoding it. The check is a SHA-256 signature made with the webhook secret over the timestamp and payload, accepts any `v1` signature in the header, and refuses a timestamp more than 300 seconds from now as a replay.
- The event id goes into `stripe_events` in the same transaction as its effect. A repeated event id changes nothing and answers 200, so Stripe's retries and a manual resend both charge and extend once. A store failure answers 500 so Stripe retries.
- An event of a type central does not handle still stores its id and answers 200, so Stripe stops resending it.
- A renewal extends `current_period_end` with `GREATEST`, so a late event never shortens a period.
- A paid invoice for a subscription that is no longer active is ignored, not revived. Stripe never revives a deleted subscription, and a local cancel or an operator's replacing write leaves the older Stripe subscription dead.
- Starting a subscription cancels any other active one on the plate and registers the vehicle to the customer, in the same transaction.
- Checkout refuses a plate held by another customer or a company. Otherwise the paid invoice would take that vehicle over.
- `checkout.session.completed` grants only where `mode` is `payment`, `payment_status` is `paid`, and the metadata `kind` is `single_wash`. A Premium sign-up completes a session too, and that one stores its event id and does nothing else, since Premium starts on `invoice.paid`.
- A prepaid wash is keyed on the Checkout session id. A repeated event stops at its stored event id, and a second event about the same session finds the prepaid wash already there and grants nothing, so handling another event about that session later cannot grant twice.
- A grant registers the plate to the buyer only when nobody holds it yet. A single purchase never moves an owned plate, unlike a paid Premium invoice.
- Checkout drops a taxi's trailing `T` with the same rule as the lane, so `ABC123T` is sold as `ABC123`. See `canon/context/plates.md`.
- The Stripe API version is pinned to `2025-03-31.basil` in `core/internal/central/stripe/client.go`, and event payloads are decoded in that shape. Keep it equal to the account's default version, since events the Stripe CLI forwards use the default. A payload missing a field central needs answers 400 and usually means a version mismatch.

## Gotchas

- A live key never starts. `NewClient` accepts only `sk_test_` and `rk_test_` prefixes, and central exits at start on anything else.
- With `STRIPE_SECRET_KEY` or `STRIPE_WEBHOOK_SECRET` unset, central starts with Stripe off and all three routes answer 503. The stack runs with no Stripe account.
- The `stripe-cli` Compose service forwards test events to `central:8080/stripe/webhook` and runs only under `docker compose --profile stripe up`. It needs the same test key.
- The event fixtures under `core/internal/central/stripe/` follow the documented shape of the pinned version. They were not captured from a real account, so a real event from the account is the check that they match.
- Nothing in central seeds a customer, a `premium` price, or a `single_wash` price. No route creates a customer and no migration inserts a `prices` row, so either checkout answers 422 for any customer and 503 for lack of its price until the rows are inserted by hand.
- An async payment method can complete a session unpaid and settle later through `checkout.session.async_payment_succeeded`, which central does not handle. Test-mode cards settle at once, so a single wash paid by card always arrives paid.
- The success and cancel URLs come from `CHECKOUT_SUCCESS_URL` and `CHECKOUT_CANCEL_URL` and default to `http://localhost:5173/`.

## Adding a handled event

Add its type and decoded shape in `core/internal/central/stripe/event.go`, map it to a `SubscriptionAction` in `subscriptionEvent` in `core/internal/central/webhook.go`, and apply it in `ApplySubscriptionEvent` in `core/internal/central/store.go`. An event that grants something other than a subscription gets its own store method beside `GrantPrepaidWash`, storing the event id through `storeEventID` in the same transaction. Take the cursor lock first, and append an entitlement change when the plate's plan or company changes.
