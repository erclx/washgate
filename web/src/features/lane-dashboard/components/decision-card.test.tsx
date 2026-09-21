import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { buildDecision } from '@/test/factories'

import { DecisionCard } from './decision-card'

describe('DecisionCard', () => {
  it('shows the decision word and the reason under it', () => {
    render(
      <DecisionCard
        decision={buildDecision({ outcome: 'pay', reason: 'cap_reached' })}
      />,
    )

    expect(screen.getByText('Pay')).toBeInTheDocument()
    expect(
      screen.getByText('Premium cap of 8 reached, offer a single wash'),
    ).toBeInTheDocument()
  })

  it('fills the card with the tint of its state', () => {
    render(
      <DecisionCard
        decision={buildDecision({ outcome: 'staff', reason: 'low_confidence' })}
      />,
    )

    expect(screen.getByTestId('decision-card')).toHaveClass('bg-staff-tint')
  })
})
