# Design

Authoring guidance: the design standard.

## Personality

washgate is calm and exact. Staff at a lane read the decision at a glance and trust it, and a driver on a phone feels the service is simple and looked after. Quiet stone surfaces, one deep petrol accent that reads as water, and a serif that gives headings some warmth. Words are plain: say what happened and why, never sell. Color means state: green admits, amber asks for payment, violet calls staff.

## Color

Stone neutrals at one cool temperature, with petrol as the only accent. The lane states admit, pay, and staff use success, warning, and staff, and never borrow the accent. Each role has a light and a dark value, and the dark ones are set separately rather than inverted from light.

| Role            | Intent                                                  | Value            |
| --------------- | ------------------------------------------------------- | ---------------- |
| background      | cool stone page canvas                                  | #f2f3f1          |
| surface         | near-white lane cards, panels, the phone frame          | #fcfcfb          |
| text            | green-black body text                                   | #1c2120          |
| muted           | stone grey captions, labels, trace timings              | #69716f          |
| accent          | deep petrol primary action and the washes-left meter    | #0f5c66          |
| success         | admit decision and online status                        | #2f7a3d          |
| warning         | pay decision                                            | #9a6a12          |
| error           | failures such as a rejected sync, never a lane decision | #b3261e ? verify |
| staff           | staff decision and the plate confirm field              | #6b3fa0          |
| border          | hairline card and input edges                           | #dcdfdb          |
| background-dark | wet slate page canvas                                   | #131716          |
| surface-dark    | raised slate lane cards and panels                      | #1a1f1e          |
| text-dark       | pale stone body text                                    | #e9eeec          |
| muted-dark      | mid stone captions and labels                           | #95a09d          |
| accent-dark     | light petrol primary action                             | #5fb3bd          |
| success-dark    | admit decision on dark                                  | #7cc48a          |
| warning-dark    | pay decision on dark                                    | #e2b454          |
| error-dark      | failures on dark                                        | #f2847a ? verify |
| staff-dark      | staff decision on dark                                  | #b99be0          |
| border-dark     | slate card and input edges                              | #2a3130          |

Decision cards fill with a tint of their state color: `#edf5ea` admit, `#faf1dc` pay, and `#f1ebf7` staff in light, and `#1c2a1d`, `#2e2513`, and `#2a2233` in dark.

## Typography

Three families, one job each: Charter for the decision word and headings, Ubuntu Sans for everything read as text, and Ubuntu Sans Mono for plates, numbers, and trace values. Charter is not on Google Fonts, so the web app self-hosts it.

| Role    | Family           | Weight       | Size          | Line height   |
| ------- | ---------------- | ------------ | ------------- | ------------- |
| display | Charter          | 700 ? verify | 30px          | 33px ? verify |
| heading | Charter          | 700 ? verify | 24px ? verify | 30px ? verify |
| body    | Ubuntu Sans      | 400 ? verify | 15px          | 22px ? verify |
| label   | Ubuntu Sans      | 400 ? verify | 12px          | 16px ? verify |
| code    | Ubuntu Sans Mono | 400 ? verify | 15px          | 22px ? verify |

## Spacing

An 8px base. Space between groups is larger than space inside them: md inside a card, lg between cards.

| Step | Multiplier | Value         |
| ---- | ---------- | ------------- |
| xs   | 0.5        | 4px ? verify  |
| sm   | 1          | 8px ? verify  |
| md   | 2          | 16px ? verify |
| lg   | 3          | 24px ? verify |
| xl   | 5          | 40px ? verify |

## Borders

| Role    | Radius | Width | When used                                 |
| ------- | ------ | ----- | ----------------------------------------- |
| default | 10px   | 1px   | cards, inputs, the phone card             |
| pill    | 8px    | 0     | decision cards, status chips, buttons     |
| plate   | 4px    | 2px   | the plate badge, dashed on an unsure read |
| none    | 0      | 0     | edge-to-edge surfaces                     |

## Motion

Motion only reports change: a new lane decision fades in over 150ms ease-out, and nothing animates on load. Proposed, not yet confirmed.

## Iconography

Outline icons from Lucide at a 1.5px stroke, used sparingly beside labels, with no custom icons. Proposed, not yet confirmed.
