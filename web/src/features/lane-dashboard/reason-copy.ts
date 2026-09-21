import type { LaneDecision } from '@/data-source/data-source'

export const PREMIUM_MONTHLY_CAP = 8

export function formatScore(score: number): string {
  return score.toFixed(2)
}

export function reasonCopy(decision: LaneDecision): string {
  switch (decision.reason) {
    case 'within_cap':
      return `Premium, wash ${decision.wash_number ?? '?'} of ${PREMIUM_MONTHLY_CAP} this month`
    case 'fleet':
      return `Fleet car, billed to ${decision.company_name ?? '?'}`
    case 'duplicate_read':
      return 'Same car read again, counted once'
    case 'cap_reached':
      return `Premium cap of ${PREMIUM_MONTHLY_CAP} reached, offer a single wash`
    case 'unknown_plate':
      return 'No subscription, single wash'
    case 'unknown_plate_offline':
      return "Not in the site's copy, which is out of date"
    case 'low_confidence':
      return `Read below the ${formatScore(decision.cutoff)} cutoff`
    case 'malformed_plate':
      return 'Plate shape not recognized'
    case 'confirmed_by_staff':
      return 'Confirmed by staff'
    case 'sent_to_pay_by_staff':
      return 'Sent to pay by staff'
  }
}
