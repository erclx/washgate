import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type {
  DataSource,
  LaneEvent,
  SiteStatus,
} from '@/data-source/data-source'
import { DataSourceContext } from '@/data-source/data-source-context'
import {
  buildDecision,
  buildLookup,
  buildStaffDecision,
  buildStatus,
} from '@/test/factories'

import { LaneDashboard } from './lane-dashboard'

interface StubHandles {
  source: DataSource
  pushLane: (event: LaneEvent) => void
  failLane: () => void
  pushStatus: (status: SiteStatus) => void
}

function buildStubSource(): StubHandles {
  const handles = {
    onEvent: (_event: LaneEvent) => {},
    onLaneUnreachable: () => {},
    onStatus: (_status: SiteStatus) => {},
  }
  const source: DataSource = {
    kind: 'live',
    lane: {
      subscribeToLane: (_siteId, onEvent, onUnreachable) => {
        handles.onEvent = onEvent
        handles.onLaneUnreachable = onUnreachable
        return () => {}
      },
      photoUrl: () => null,
      confirmPlate: vi.fn(async () => {}),
      sendToPay: vi.fn(async () => {}),
      enabledStaffActions: () => ['confirm', 'send_to_pay'],
    },
    site: {
      sites: () => [{ id: 'site-1', name: 'Site 1' }],
      subscribeToStatus: (_siteId, onStatus) => {
        handles.onStatus = onStatus
        return () => {}
      },
      setLinkCut: vi.fn(async () => {}),
      canSwitchLink: () => true,
    },
    lookup: {
      lookUpPlate: vi.fn(async () => ({
        isFound: true as const,
        lookup: buildLookup(),
      })),
    },
  }
  return {
    source,
    pushLane: (event) => act(() => handles.onEvent(event)),
    failLane: () => act(() => handles.onLaneUnreachable()),
    pushStatus: (status) => act(() => handles.onStatus(status)),
  }
}

function renderDashboard(stub: StubHandles) {
  render(
    <DataSourceContext value={stub.source}>
      <LaneDashboard />
    </DataSourceContext>,
  )
}

describe('LaneDashboard', () => {
  it('shows the waiting line until the site decides a car', () => {
    const stub = buildStubSource()
    renderDashboard(stub)

    expect(
      screen.getByText(
        'No cars yet. Decisions appear here as the lane reads them.',
      ),
    ).toBeInTheDocument()
  })

  it('shows a new decision as the current car', () => {
    const stub = buildStubSource()
    renderDashboard(stub)

    stub.pushLane({
      type: 'decision',
      body: buildDecision({ reason: 'within_cap', wash_number: 5 }),
    })

    expect(
      screen.getByRole('article', { name: 'Current car' }),
    ).toHaveTextContent('Premium, wash 5 of 8 this month')
  })

  it('replaces the lane feed with the unreachable line and greys the last health', () => {
    const stub = buildStubSource()
    renderDashboard(stub)
    stub.pushStatus(buildStatus())

    stub.failLane()

    expect(
      screen.getByText(
        "Can't reach the site agent for Site 1. Showing the last known state.",
      ),
    ).toBeInTheDocument()
    expect(screen.getByTestId('health-strip')).toHaveAttribute(
      'data-stale',
      'true',
    )
  })

  it('sends a staff confirm for the current car to its site', async () => {
    const user = userEvent.setup()
    const stub = buildStubSource()
    renderDashboard(stub)
    stub.pushLane({
      type: 'decision',
      body: buildStaffDecision({ id: 'dec-staff', plate: 'MLB482' }),
    })

    await user.click(screen.getByRole('button', { name: 'Confirm' }))

    expect(stub.source.lane.confirmPlate).toHaveBeenCalledWith(
      'site-1',
      'dec-staff',
      'MLB482',
    )
  })

  it('opens the lookup panel from the plate search without touching the lane feed', async () => {
    const user = userEvent.setup()
    const stub = buildStubSource()
    renderDashboard(stub)

    await user.type(
      screen.getByPlaceholderText('Search a plate'),
      'ABC123{Enter}',
    )

    expect(await screen.findByText('Premium, active')).toBeInTheDocument()
    expect(
      screen.getByText(
        'No cars yet. Decisions appear here as the lane reads them.',
      ),
    ).toBeInTheDocument()
  })

  it('announces a replay build with its banner', () => {
    const stub = buildStubSource()
    renderDashboard({ ...stub, source: { ...stub.source, kind: 'replay' } })

    expect(screen.getByText('Replay of a recorded run')).toBeInTheDocument()
  })

  it('credits the replay photo with a link to its attribution', () => {
    const stub = buildStubSource()
    renderDashboard({ ...stub, source: { ...stub.source, kind: 'replay' } })

    expect(
      screen.getByRole('link', { name: 'Photo: nakhon100, CC BY 2.0' }),
    ).toHaveAttribute('href', '/photo-attribution.md')
  })

  it('shows no photo credit outside a replay build', () => {
    renderDashboard(buildStubSource())

    expect(
      screen.queryByRole('link', { name: 'Photo: nakhon100, CC BY 2.0' }),
    ).not.toBeInTheDocument()
  })
})
