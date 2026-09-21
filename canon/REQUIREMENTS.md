# Requirements

## Problem

A staffed car wash recognizes a car at the entrance and lets it drive straight into the hall. Behind that sit subscriptions with a monthly wash cap, company fleets billed per car, pay-per-wash for everyone else, and 20 sites that must keep working when their link to head office drops. Getting any of it wrong turns away a paying customer, lets in a car that should pay, or bills the wrong account.

washgate is a working model of that system, built to show how the backend stays correct when conditions are bad. It is a reading of the problem any staffed car wash chain has, not a copy of any one company's system.

## Goals

- A car at the lane gets a decision at once: admit, pay, or ask staff, with the reason shown.
- Every wash is counted exactly once, whatever the camera, the network, or the payment provider repeats.
- A site keeps admitting cars while offline and reconciles with head office when the link returns.
- A plate read the system is unsure of goes to the staff on the lane rather than being guessed.
- Head office can look up any plate, fix a quota, and produce the monthly fleet invoice.
- A reviewer can follow one car from sign-up to the wash gate and see what the backend did at each step.

## Non-goals

- Training a plate recognition model. A pretrained model stands in for the camera vendor's.
- A native mobile app or real customer accounts. A web mockup of the customer app replaces them.
- Swish payments, which need a company agreement.
- FileMaker, which is proprietary and licensed per server.
- Moving real money. Payments run in Stripe test mode.

## MVP features

1. Lane decision: a site admits a Premium subscriber within the monthly cap of eight washes.
2. Cap reached: a Premium subscriber past the cap is offered a single wash instead.
3. Fleet wash: a company car is admitted and logged against the company.
4. Unknown plate: a car with no subscription is routed to pay-per-wash.
5. Duplicate read: the camera firing twice for one car counts one wash.
6. Offline site: a site keeps deciding from its local copy and syncs when the link returns.
7. Repeated payment event: the payment provider sending an event twice charges and extends once.
8. Uncertain read: a low-confidence or malformed plate read goes to staff.
9. Monthly invoice: head office exports one line per car per company, split by leasing company on request.
10. Customer journey: a driver registers a plate and starts Premium in the app mockup, and the lane admits the car.

## Tech stack

- Go
- PHP
- Python
- React with Vite and TypeScript
- MariaDB
- SQLite
- Stripe test mode
- Docker Compose
- Bun for scripts and hooks

## Constraints

- Only the camera and the customers are simulated. Every service, database, and network call runs for real.
- Test images are openly licensed public images only, kept with their attribution.
- No image of a plate is stored after it is read.
- One command starts the whole system on a developer machine.
