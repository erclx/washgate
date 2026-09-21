---
title: Lane dashboard
description: The live view of one site's lane, with each car's decision, its backend trace, the staff prompt, site health, and plate lookup
---

# Lane dashboard

The lane dashboard shows what one site decided for each car and what the backend did to reach it. Staff read the newest decision at a glance and answer the staff prompt when a read is uncertain. A reviewer reads the trace beside each car, cuts the site's link to head office, and watches the outbox fill and drain. It covers one site at a time, chosen from the top bar, plus a plate lookup against head office.

## Regions

- Top bar: product name, site picker, plate search, and a link to the customer app, across the full width at the top
- Site health strip: online or offline, outbox depth, last sync, and the cut-link switch, directly under the top bar
- Current car: the newest decision as a full card, at the top of the lane feed under the health strip
  - Lane photo: the frame the plate was read from, at the card's left
  - Plate read: the plate text as a plate badge with its confidence, right of the photo
  - Decision: admit, pay, or staff in large type on a fill of its state color, with the reason under it, right of the plate read
  - Trace: the backend steps for this car, one per line with its result, at the card's right
  - Staff prompt: replaces the trace when the decision is staff, holding the plate field and the two actions
- Earlier cars: one row per car under the current car, newest first, with plate, decision, reason, and time, and no photo
- Lookup panel: the result of a plate search, a column right of the lane feed
- At below 1280 wide: the lookup panel opens over the lane feed from the plate search and closes back to it

```plaintext
┌──────────────────────────────────────────────────────────────────────────────────────┐
│ washgate   [Site 07 ▾]              [ Search a plate ]              Customer app →   │ ← top bar
├──────────────────────────────────────────────────────────────────────────────────────┤
│ ● Online   Outbox 0   Synced 2 s ago                                   [ Cut link ]  │ ← site health strip
├───────────────────────────────────────────────────────────────┬──────────────────────┤
│ ┌─────────┐  Plate read       ┌──────────────┐  Trace         │ Lookup               │
│ │  lane   │  ┌─────────┐      │ Admit        │  Entitlement ✓ │ ┌─────────┐          │
│ │  photo  │  │ ABC 123 │      │ Premium,     │  Ledger      ✓ │ │ ABC 123 │          │
│ │         │  └─────────┘      │ wash 5 of 8  │  Outbox      ✓ │ └─────────┘          │
│ └─────────┘  Confidence 0.97  └──────────────┘  Sync to HQ   ✓ │ Premium, active      │
│                                                                │ 5 of 8 this month    │
│  ← current car: photo, plate read, decision, trace             │ Washes this month    │
├───────────────────────────────────────────────────────────────┤  12 Sep  Site 07     │
│ KTR 55T   Pay     No subscription, single wash       14:31:52 │  09 Sep  Site 03     │
│ MLB 482   Admit   Confirmed by staff                 14:30:10 │  ...                 │
│ ABC 123   Admit   Same car read again, counted once  14:29:58 │                      │
│  ← earlier cars, newest first                                  │ ← lookup panel       │
└───────────────────────────────────────────────────────────────┴──────────────────────┘
```

## States

| State            | Reached when                                                        | Shows                                                                                              | Evidence     |
| ---------------- | ------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | ------------ |
| `waiting`        | the site has decided no car since the dashboard opened              | the health strip and an empty lane feed with the waiting line                                      | not captured |
| `admitted`       | the newest car is admitted within cap or as a fleet car             | the current car with an admit decision, its reason, and a four-step trace ending in sync to HQ     | not captured |
| `duplicate-read` | the camera reads a car already admitted inside the dedup window     | an admit decision with the duplicate reason, and a trace showing no new ledger write               | not captured |
| `pay`            | the plate has no subscription or the Premium cap is reached         | a pay decision with its reason, and a trace ending at the lookup                                   | not captured |
| `staff`          | a read is below the confidence cutoff or not in a known plate shape | a staff decision and the staff prompt in place of the trace                                        | not captured |
| `offline`        | the link to head office is cut or drops                             | the health strip reads offline, the outbox count climbs with each wash, and sync steps read queued | not captured |
| `draining`       | the link returns with washes waiting in the outbox                  | the health strip reads syncing and the outbox count falls to zero                                  | not captured |
| `agent-down`     | the dashboard cannot reach the site agent                           | the lane feed replaced by the unreachable line, with the last known health greyed                  | not captured |
| `lookup-found`   | a plate search matches a vehicle                                    | the lookup panel with owner type, subscription, and this month's washes                            | not captured |
| `lookup-none`    | a plate search matches nothing                                      | the lookup panel with the no-match line                                                            | not captured |

### staff

The staff prompt shows the plate the reader returned, prefilled in an editable field, with the photo still beside it. Confirm sends the corrected plate back through the same decision, so the car ends admitted or pay like any other. Send to pay skips the lookup and routes the car to a single wash. Until staff answer, the card stays the current car, and newer cars queue under it rather than replacing it.

## Copy

- Top bar: `washgate`, `Search a plate`, `Customer app`
- Health strip: `Online`, `Offline`, `Syncing`, `Outbox <n>`, `Synced <time> ago`, `Cut link`, `Restore link`
- Decisions: `Admit`, `Pay`, `Staff`
- Reasons, one per backend reason:
  - within cap: `Premium, wash <n> of 8 this month`
  - fleet: `Fleet car, billed to <company>`
  - duplicate read: `Same car read again, counted once`
  - cap reached: `Premium cap of 8 reached, offer a single wash`
  - unknown plate: `No subscription, single wash`
  - low confidence: `Read below the <cutoff> cutoff`
  - malformed plate: `Plate shape not recognized`
- Trace steps: `Entitlement lookup`, `Ledger write`, `Outbox entry`, `Sync to HQ`
- Staff prompt: `Confirm the plate`, `Confirm`, `Send to pay`, and after a confirm the reason `Confirmed by staff`
- Waiting line: `No cars yet. Decisions appear here as the lane reads them.`
- Unreachable line: `Can't reach the site agent for <site>. Showing the last known state.`
- Lookup: `Washes this month`, and the no-match line `No vehicle registered with <plate>.`
- `<n>`, `<time>`, `<company>`, `<cutoff>`, `<site>`, and `<plate>` are filled from live data and are not final text.

## Behavior

- A new decision takes the current car's place and pushes the previous car down into the earlier cars list.
- Picking a site in the top bar switches the whole view to that site's lane and health.
- Cut link stops the site's sync to head office. The site keeps deciding, and the button reads Restore link until pressed again.
- Answering the staff prompt resolves the current car, and any cars queued behind it take the current car's place in order.
- Searching a plate opens the lookup panel. It never changes the lane feed.

## Not on this surface

- No deny outcome. A car is admitted, sent to pay, or sent to staff.
- No stored photos. Only the current car shows its lane photo, and earlier cars show none, since no plate image is kept after the read.
- No quota reset or invoice export, which `washctl` and the invoicing service own.
- No charts or daily totals.
- No view of more than one site at once.
