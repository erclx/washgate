import { describe, expect, it } from 'vitest'

import { buildDecision } from '@/test/factories'

import { reasonCopy } from './reason-copy'

describe('reasonCopy', () => {
  it.each([
    ['duplicate_read', 'Same car read again, counted once'],
    ['cap_reached', 'Premium cap of 8 reached, offer a single wash'],
    ['unknown_plate', 'No subscription, single wash'],
    ['unknown_plate_offline', "Not in the site's copy, which is out of date"],
    ['malformed_plate', 'Plate shape not recognized'],
    ['confirmed_by_staff', 'Confirmed by staff'],
    ['sent_to_pay_by_staff', 'Sent to pay by staff'],
  ] as const)('reads %s as the wireframe copy', (reason, copy) => {
    expect(reasonCopy(buildDecision({ reason }))).toBe(copy)
  })

  it('fills the wash number into the within-cap reason', () => {
    expect(
      reasonCopy(buildDecision({ reason: 'within_cap', wash_number: 5 })),
    ).toBe('Premium, wash 5 of 8 this month')
  })

  it('fills the company into the fleet reason', () => {
    expect(
      reasonCopy(
        buildDecision({ reason: 'fleet', company_name: 'Nordfrakt AB' }),
      ),
    ).toBe('Fleet car, billed to Nordfrakt AB')
  })

  it('fills the cutoff into the low-confidence reason', () => {
    expect(
      reasonCopy(buildDecision({ reason: 'low_confidence', cutoff: 0.8 })),
    ).toBe('Read below the 0.80 cutoff')
  })
})
