---
title: Plates
description: Plate normalization rule and where it is copied, plate reader output and confidence, the site's confidence cutoff, and the test image with its attribution
---

# Plates

## Overview

A plate is the key every other domain looks a car up by, so one string has to come out the same at the reader, the lane, and central. The Python plate reader turns a photo into text and a confidence score and decides nothing. The site agent normalizes that text, checks its shape, and applies the confidence cutoff.

## Layout

- `plate-reader/src/plate_reader/` owns the reader, the send-on to the site agent, and the response models
- `plate-reader/tests/fixtures/` owns the public test image and its attribution
- `core/internal/siteagent/` owns the normalization rule the lane uses
- `core/internal/central/` owns central's own copies of the rule, at checkout, at the ledger, and on the operator routes

## Normalization

A raw read becomes a key in this order:

1. Upper-case it and drop spaces and hyphens.
2. Accept it only if what remains is two to seven characters of `A` to `Z` and `0` to `9`. Anything else is malformed and goes to staff.
3. If it matches three letters, two digits, and one more letter or digit, followed by a `T`, drop that `T`. A taxi's trailing `T` is a marking, not part of the registration.

The rule is deliberately loose. Personalized plates run two to seven letters or digits, which contains the standard shapes, so after normalization the shape check reduces to length and character set.

### Where the rule is copied

- `core/internal/siteagent/plate.go` holds the full rule, `NormalizePlate`
- `core/internal/central/plate.go` holds the full rule again as `canonicalPlate`, for every central route that takes a typed plate
- `core/internal/central/store.go` holds the shape check again as `normalizedPlate`, so central refuses to store a plate no lane could look up
- `plate-reader/src/plate_reader/reader.py` upper-cases and drops spaces only. It drops hyphens and applies the shape check nowhere, so a hyphen reaches the site agent and is removed there

Components cannot share the rule. The reader is Python, and the Go components keep to `internal/<component>/` with no imports between them, so a change to the rule is a change in each copy. The site and central must agree on the shape and on the taxi rule.

Every central route that takes a typed plate runs `canonicalPlate`: the operator routes, both checkout routes, and the customer routes. `ABC 123` registers, checks out, and looks up under `ABC123`, the key the lane uses. A plate registered by one customer and bought by another is refused at checkout only because both routes normalize alike.

## Confidence

`confidence` is the OCR confidence for the plate the model detected with the highest detection confidence, averaged over its characters. The reader returns it raw, from 0 to 1, and never decides admit, pay, or staff.

The site admits a read only at or above `SITE_MIN_CONFIDENCE`, which defaults to `0.99` and is read in `core/cmd/site-agent/main.go`. A read below it goes to staff as `low_confidence` before the shape is checked.

The default comes from scoring the model against 56 hand-labeled Wikimedia Commons plates. At 0.90 it accepted 50 reads and 5 were wrong. At 0.99 it accepted 46, 2 were wrong, and 10 went to staff. The higher cutoff costs more staff prompts and buys fewer wrong admits.

## Decisions

- The cutoff lives at the site and not in the reader, so a site tunes it without a new reader image and the reader stays a measurement.
- Confidence does not catch every misread. A different car's plate read cleanly with high confidence still passes both checks. The lookup limits the damage, because a misread matching no subscriber falls through to pay-per-wash. A misread that lands on another subscriber's plate is the case the system does not catch, and `canon/ARCHITECTURE.md` lists how often it can happen as open.
- The reader returns nothing for a photo with no plate. `POST /read` answers 404 and sends nothing on. An empty body answers 422 and an image over 10 MiB answers 413.

## Gotchas

- A local `uv run` downloads the model weights on the first read, so it needs the network once. The Docker image downloads them at build, so a container reads with the network down.
- The reader posts the photo to the site agent as base64 beside the read. The site agent caps `POST /reads` at 14 MiB to fit the reader's 10 MiB image after base64, so raising the reader's cap means raising the site's with it. See `canon/context/lane.md`.
- The reader retries a failed send three times with a growing pause and a two second timeout each, so a site agent that is down adds a few seconds to the reader's answer.

## Test image

The reader test reads one real photo and expects `EEK828`. The image is `plate-reader/tests/fixtures/31-Volvo-S90-3.0-15285826772-.jpg`, from Wikimedia Commons under CC BY 2.0, and `plate-reader/tests/fixtures/ATTRIBUTION.md` carries the title, author, license, and source. The image is in the repository so the test needs no network for it. Any image added later must be openly licensed and gets an entry in that file.
