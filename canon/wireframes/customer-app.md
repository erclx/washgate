---
title: Customer app
description: The phone-sized mockup of the customer app, a separate route in the dashboard app, where a demo customer registers a plate, starts Premium, and sees washes left
---

# Customer app

The customer app mockup stands in for the chain's phone app so a reviewer can follow one car from sign-up to the lane. A demo customer from the seed data replaces a login. The customer registers a plate, starts Premium through Stripe Checkout in test mode, and sees how many of the month's eight washes are left. A car with no subscription can buy a single wash instead. Every screen talks to the central API.

## Regions

- Reviewer note: one line above the phone frame saying this is a mockup, with a link back to the lane dashboard
- Phone frame: a phone-width column centered on the page, holding every screen below
- App header: product name and the demo customer's name with a switch link, at the top of the phone frame
- Customer picker: the seed customers as a list, each with a short summary of their cars, the first screen before any customer is picked
- Car list: one card per registered plate under the app header, each opening that car's detail
  - Car card: the plate badge and the plan name, and for Premium the washes left with a meter of the eight, or for no plan a quiet Start Premium hint
- Add plate: a plate field and an add action, under the car list
- Car detail: one car's plate, plan, washes left, reset date, and this month's washes, reached from its card, with a back link to the car list
  - Plan offer: for a car with no plan, what Premium gives, above the plan actions
  - Plan actions: Start Premium and Buy a single wash, at the bottom of the car detail
- At a phone-width viewport: the phone frame fills the screen and the reviewer note hides

```plaintext
          This is a mockup of the customer app.  Back to the lane dashboard →       ← reviewer note
                    ┌────────────────────────────────────┐
                    │ washgate   Demo: <customer> Switch │ ← app header
                    ├────────────────────────────────────┤
                    │ Your cars                          │
                    │ ┌────────────────────────────────┐ │
                    │ │ ┌─────────┐          Premium   │ │
                    │ │ │ ABC 123 │                    │ │ ← car card, Premium
                    │ │ └─────────┘                    │ │
                    │ │ 3 washes left                  │ │
                    │ │ ██████████░░░░░░               │ │
                    │ │ 5 of 8 used, resets 1 Oct      │ │
                    │ └────────────────────────────────┘ │
                    │ ┌────────────────────────────────┐ │
                    │ │ ┌─────────┐          No plan   │ │ ← car card, no plan
                    │ │ │ KTR 55T │    Start Premium → │ │
                    │ │ └─────────┘                    │ │
                    │ └────────────────────────────────┘ │
                    │                                    │
                    │ Add a plate                        │
                    │ [ ABC123              ] [ Add ]    │ ← add plate
                    └────────────────────────────────────┘
```

## States

| State                | Reached when                                                      | Shows                                                                                               | Evidence     |
| -------------------- | ----------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | ------------ |
| `pick-customer`      | the route opens with no demo customer chosen                      | the customer picker                                                                                 | not captured |
| `no-cars`            | the chosen customer has no plate registered                       | the empty car list line and the add plate field                                                     | not captured |
| `no-plan`            | a car has no subscription                                         | its card reads No plan with the Start Premium hint, and its detail shows the plan offer and actions | not captured |
| `premium`            | a car has an active Premium subscription under the cap            | washes left, the meter of eight, the reset date, and this month's washes                            | not captured |
| `cap-reached`        | a Premium car has used all eight washes this month                | zero washes left, the full meter, and Buy a single wash                                             | not captured |
| `plate-invalid`      | the entered plate matches no known plate shape                    | the add plate field with the shape error under it                                                   | not captured |
| `plate-taken`        | the entered plate is already registered to another customer       | the add plate field with the taken error under it                                                   | not captured |
| `checkout-pending`   | the customer returns from Checkout before the payment event lands | the car detail with the activating line and Premium marked as activating                            | not captured |
| `checkout-cancelled` | the customer leaves Checkout without paying                       | the car detail unchanged, with the cancelled line                                                   | not captured |
| `single-wash-ready`  | a single wash is paid for a car                                   | the car card and detail reading one wash ready                                                      | not captured |
| `api-down`           | the central API does not answer                                   | the unreachable line in place of the car list                                                       | not captured |

### checkout-pending

Returning from Checkout does not make a car Premium. The car turns Premium only when the payment event reaches the central API, so the detail shows the activating line until then and updates in place when it lands. A payment event that arrives twice changes nothing on screen.

## Copy

- Reviewer note: `This is a mockup of the customer app.`, `Back to the lane dashboard`
- App header: `washgate`, `Demo: <customer>`, `Switch`
- Customer picker: `Pick a demo customer`, with each row's summary such as `1 car, Premium` or `no cars`
- Car list: `Your cars`, and the empty line `No cars yet. Add a plate to get started.`
- Car card and detail: `Premium`, `No plan`, `Start Premium`, `<n> washes left`, `<used> of 8 used, resets <date>`, `Washes this month`, `One wash ready`, `Your cars`
- Plan offer: `Premium`, `8 washes a month`, `Price shown at checkout`
- Add plate: `Add a plate`, `Add`, and the errors `That doesn't look like a Swedish plate.` and `This plate is already registered.`
- Plan actions: `Start Premium`, `Buy a single wash`
- Checkout return: `Activating Premium. This takes a few seconds.` and `Checkout cancelled. Nothing was charged.`
- Unreachable line: `Can't reach washgate right now. Try again in a moment.`
- `<customer>`, `<n>`, `<used>`, and `<date>` are filled from live data and are not final text. Demo customer names come from the seed data.
- The Premium price and the single-wash price come from the central API and are not written here.

## Behavior

- Picking a demo customer opens their car list. Switch returns to the picker.
- Tapping a car card opens its detail, and the back link returns to the car list.
- Adding a plate checks its shape before sending it, and a valid plate appears as a new card with no plan.
- Start Premium leaves for Stripe Checkout in test mode and returns to the car detail.
- Buy a single wash also goes through Checkout, and the car's next read at any lane admits it once.
- Washes left counts down as the lane admits the car, and the reset date is the first of next month in Stockholm time.

## Not on this surface

- No real login or account settings. A demo customer from the seed data stands in.
- No Swish, only Stripe test mode.
- No lane photos or decision reasons. Those stay on the lane dashboard.
- No company fleet cars, which head office manages.
- No vacuum or mat washer controls.
