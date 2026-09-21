import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { buildLookup } from '@/test/factories'

import { LookupPanel } from './lookup-panel'

const sites = [{ id: 'site-1', name: 'Site 1' }]

describe('LookupPanel', () => {
  it('shows a Premium plate with its subscription, count against the cap, and washes this month', () => {
    render(
      <LookupPanel
        lookup={{ status: 'found', lookup: buildLookup() }}
        sites={sites}
        onClose={vi.fn()}
      />,
    )

    expect(screen.getByText('ABC 123')).toBeInTheDocument()
    expect(screen.getByText('Premium, active')).toBeInTheDocument()
    expect(screen.getByText('5 of 8 this month')).toBeInTheDocument()
    expect(
      screen.getByRole('heading', { name: 'Washes this month' }),
    ).toBeInTheDocument()
    expect(screen.getByText('12 Sep')).toBeInTheDocument()
  })

  it('shows a company car as billed to its company with no cap', () => {
    const lookup = buildLookup({
      owner_type: 'company',
      owner_name: 'Nordfrakt AB',
      subscription: null,
      washes_this_month: 1,
    })

    render(
      <LookupPanel
        lookup={{ status: 'found', lookup }}
        sites={sites}
        onClose={vi.fn()}
      />,
    )

    expect(
      screen.getByText('Fleet car, billed to Nordfrakt AB'),
    ).toBeInTheDocument()
    expect(screen.getByText('1 this month')).toBeInTheDocument()
  })

  it('shows the no-owner line for a registered vehicle with no owner', () => {
    const lookup = buildLookup({
      owner_type: null,
      owner_name: null,
      subscription: null,
      washes_this_month: 0,
      washes: [],
    })

    render(
      <LookupPanel
        lookup={{ status: 'found', lookup }}
        sites={sites}
        onClose={vi.fn()}
      />,
    )

    expect(screen.getByText('No owner on record')).toBeInTheDocument()
    expect(screen.getByText('0 this month')).toBeInTheDocument()
  })

  it('shows the no-match line for a plate nobody registered', () => {
    render(
      <LookupPanel
        lookup={{ status: 'none', plate: 'ZZZ999' }}
        sites={sites}
        onClose={vi.fn()}
      />,
    )

    expect(
      screen.getByText('No vehicle registered with ZZZ999.'),
    ).toBeInTheDocument()
  })

  it('says head office is out of reach when the lookup fails', () => {
    render(
      <LookupPanel
        lookup={{ status: 'error' }}
        sites={sites}
        onClose={vi.fn()}
      />,
    )

    expect(screen.getByRole('alert')).toHaveTextContent(
      "Can't reach head office.",
    )
  })

  it('closes back to the lane feed', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(
      <LookupPanel
        lookup={{ status: 'none', plate: 'ZZZ999' }}
        sites={sites}
        onClose={onClose}
      />,
    )

    await user.click(screen.getByRole('button', { name: 'Close' }))

    expect(onClose).toHaveBeenCalledOnce()
  })
})
