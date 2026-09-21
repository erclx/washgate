import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { SiteStatus } from '@/data-source/data-source'
import { buildStatus } from '@/test/factories'

import { HealthStrip } from './health-strip'

const NOW = Date.parse('2026-09-21T12:30:00Z')

function renderStrip(
  status: SiteStatus | null,
  { isAgentDown = false, canSwitchLink = true, onToggleLink = vi.fn() } = {},
) {
  render(
    <HealthStrip
      status={status}
      isAgentDown={isAgentDown}
      canSwitchLink={canSwitchLink}
      onToggleLink={onToggleLink}
      now={NOW}
    />,
  )
  return { onToggleLink }
}

describe('HealthStrip', () => {
  it('shows an online site with its outbox and the age of its last sync', () => {
    renderStrip(
      buildStatus({ outbox_depth: 0, last_synced_at: '2026-09-21T12:29:58Z' }),
    )

    expect(screen.getByText('Online')).toBeInTheDocument()
    expect(screen.getByText('Outbox 0')).toBeInTheDocument()
    expect(screen.getByText('Synced 2 s ago')).toBeInTheDocument()
  })

  it('reads a cut link as offline and offers to restore it', () => {
    renderStrip(buildStatus({ link: 'cut', outbox_depth: 2 }))

    expect(screen.getByText('Offline')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Restore link' }),
    ).toBeInTheDocument()
  })

  it('reads a draining outbox as syncing', () => {
    renderStrip(buildStatus({ is_syncing: true, outbox_depth: 2 }))

    expect(screen.getByText('Syncing')).toBeInTheDocument()
  })

  it('asks to cut the link when the switch is pressed on a live site', async () => {
    const user = userEvent.setup()
    const { onToggleLink } = renderStrip(buildStatus())

    await user.click(screen.getByRole('button', { name: 'Cut link' }))

    expect(onToggleLink).toHaveBeenCalledWith(true)
  })

  it('disables the switch where the data source cannot cut the link', () => {
    renderStrip(buildStatus(), { canSwitchLink: false })

    expect(screen.getByRole('button', { name: 'Cut link' })).toBeDisabled()
  })

  it('greys the last known health when the site agent is unreachable', () => {
    renderStrip(buildStatus(), { isAgentDown: true })

    expect(screen.getByTestId('health-strip')).toHaveAttribute(
      'data-stale',
      'true',
    )
  })
})
