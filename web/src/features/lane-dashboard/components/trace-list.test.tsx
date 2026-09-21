import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { buildTrace } from '@/test/factories'

import { TraceList } from './trace-list'

describe('TraceList', () => {
  it('lists the four backend steps in order', () => {
    render(<TraceList trace={buildTrace()} />)

    const labels = screen
      .getAllByRole('listitem')
      .map((item) => item.firstChild?.textContent)
    expect(labels).toEqual([
      'Entitlement lookup',
      'Ledger write',
      'Outbox entry',
      'Sync to HQ',
    ])
  })

  it('reads a sync waiting on the outbox as queued', () => {
    render(<TraceList trace={buildTrace({ sync_to_hq: 'queued' })} />)

    const syncRow = screen.getByText('Sync to HQ').closest('li') as HTMLElement
    expect(within(syncRow).getByText('Queued')).toBeInTheDocument()
  })

  it('reads a step the decision never reached as skipped', () => {
    render(<TraceList trace={buildTrace({ ledger_write: 'skipped' })} />)

    const ledgerRow = screen
      .getByText('Ledger write')
      .closest('li') as HTMLElement
    expect(within(ledgerRow).getByText('Skipped')).toBeInTheDocument()
  })
})
