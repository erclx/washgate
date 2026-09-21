import type { Outcome } from '@/data-source/data-source'

interface OutcomeStyle {
  word: string
  textClass: string
  tintClass: string
}

export const OUTCOME_STYLES: Record<Outcome, OutcomeStyle> = {
  admit: {
    word: 'Admit',
    textClass: 'text-success',
    tintClass: 'bg-admit-tint',
  },
  pay: { word: 'Pay', textClass: 'text-warning', tintClass: 'bg-pay-tint' },
  staff: { word: 'Staff', textClass: 'text-staff', tintClass: 'bg-staff-tint' },
}
