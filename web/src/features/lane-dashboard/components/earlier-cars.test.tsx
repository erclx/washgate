import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { buildDecision } from '@/test/factories'

import { EarlierCars } from './earlier-cars'

describe('EarlierCars', () => {
  it('lists each earlier car with its plate, decision, reason, and site time, newest first', () => {
    render(
      <EarlierCars
        queued={[]}
        earlier={[
          buildDecision({
            id: 'b',
            plate: 'KTR55T',
            outcome: 'pay',
            reason: 'unknown_plate',
            decided_at: '2026-09-21T12:31:52Z',
          }),
          buildDecision({ id: 'a', plate: 'ABC123', reason: 'duplicate_read' }),
        ]}
      />,
    )

    const [newest, oldest] = screen.getAllByRole('listitem')
    expect(within(newest).getByText('KTR 55T')).toBeInTheDocument()
    expect(within(newest).getByText('Pay')).toBeInTheDocument()
    expect(
      within(newest).getByText('No subscription, single wash'),
    ).toBeInTheDocument()
    expect(within(newest).getByText('14:31:52')).toBeInTheDocument()
    expect(within(oldest).getByText('ABC 123')).toBeInTheDocument()
  })

  it('marks cars waiting behind an open staff decision as queued', () => {
    render(
      <EarlierCars
        queued={[buildDecision({ id: 'q', plate: 'DEF456' })]}
        earlier={[]}
      />,
    )

    const row = screen.getByText('DEF 456').closest('li') as HTMLElement
    expect(within(row).getByText('Queued behind staff')).toBeInTheDocument()
  })

  it('renders nothing when no car has passed yet', () => {
    const { container } = render(<EarlierCars queued={[]} earlier={[]} />)

    expect(container).toBeEmptyDOMElement()
  })
})
