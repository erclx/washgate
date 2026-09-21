import { describe, expect, it } from 'vitest'

import type { LaneEvent } from '@/data-source/data-source'
import { buildDecision, buildStaffDecision, buildTrace } from '@/test/factories'

import { EMPTY_LANE_FEED, type LaneFeed, laneFeedReducer } from './lane-feed'

function applyEvents(
  events: LaneEvent[],
  feed: LaneFeed = EMPTY_LANE_FEED,
): LaneFeed {
  return events.reduce(laneFeedReducer, feed)
}

const ids = (decisions: readonly { id: string }[]) =>
  decisions.map((decision) => decision.id)

describe('laneFeedReducer on a new decision', () => {
  it('makes the new car current and pushes the previous car to the top of earlier cars', () => {
    const feed = applyEvents([
      { type: 'decision', body: buildDecision({ id: 'a' }) },
      { type: 'decision', body: buildDecision({ id: 'b' }) },
      { type: 'decision', body: buildDecision({ id: 'c' }) },
    ])

    expect(feed.current?.id).toBe('c')
    expect(ids(feed.earlier)).toEqual(['b', 'a'])
  })

  it('queues newer cars behind an open staff decision rather than replacing it', () => {
    const feed = applyEvents([
      { type: 'decision', body: buildStaffDecision({ id: 'staff' }) },
      { type: 'decision', body: buildDecision({ id: 'b' }) },
    ])

    expect(feed.current?.id).toBe('staff')
    expect(ids(feed.queued)).toEqual(['b'])
  })

  it('replaces a car it already holds instead of adding it twice', () => {
    const feed = applyEvents([
      { type: 'decision', body: buildStaffDecision({ id: 'staff' }) },
      {
        type: 'decision',
        body: buildStaffDecision({ id: 'staff', plate: 'MLB482' }),
      },
    ])

    expect(feed.current?.plate).toBe('MLB482')
    expect(feed.queued).toEqual([])
  })
})

describe('laneFeedReducer on a resolved staff decision', () => {
  it('lets queued cars take the current place in order once staff answer', () => {
    const feed = applyEvents([
      { type: 'decision', body: buildStaffDecision({ id: 'staff' }) },
      { type: 'decision', body: buildDecision({ id: 'b' }) },
      { type: 'decision', body: buildDecision({ id: 'c' }) },
      {
        type: 'resolved',
        body: buildDecision({ id: 'staff', reason: 'confirmed_by_staff' }),
      },
    ])

    expect(feed.current?.id).toBe('c')
    expect(ids(feed.earlier)).toEqual(['b', 'staff'])
    expect(feed.earlier[1].reason).toBe('confirmed_by_staff')
  })

  it('stops draining the queue at the next staff decision', () => {
    const feed = applyEvents([
      { type: 'decision', body: buildStaffDecision({ id: 'staff-1' }) },
      { type: 'decision', body: buildStaffDecision({ id: 'staff-2' }) },
      { type: 'decision', body: buildDecision({ id: 'c' }) },
      { type: 'resolved', body: buildDecision({ id: 'staff-1' }) },
    ])

    expect(feed.current?.id).toBe('staff-2')
    expect(ids(feed.queued)).toEqual(['c'])
  })

  it('keeps the resolved car current when nothing queued behind it', () => {
    const feed = applyEvents([
      { type: 'decision', body: buildStaffDecision({ id: 'staff' }) },
      {
        type: 'resolved',
        body: buildDecision({
          id: 'staff',
          outcome: 'pay',
          reason: 'sent_to_pay_by_staff',
        }),
      },
    ])

    expect(feed.current).toMatchObject({ id: 'staff', outcome: 'pay' })
  })
})

describe('laneFeedReducer on a sync', () => {
  it('marks the sync step done for exactly the named washes', () => {
    const feed = applyEvents([
      {
        type: 'decision',
        body: buildDecision({
          id: 'a',
          wash_id: 'w-a',
          trace: buildTrace({ sync_to_hq: 'queued' }),
        }),
      },
      {
        type: 'decision',
        body: buildDecision({
          id: 'b',
          wash_id: 'w-b',
          trace: buildTrace({ sync_to_hq: 'queued' }),
        }),
      },
      { type: 'synced', body: { wash_ids: ['w-a'] } },
    ])

    const syncStatus = (decision: LaneFeed['earlier'][number] | null) =>
      decision?.trace.find((step) => step.step === 'sync_to_hq')?.status

    expect(syncStatus(feed.earlier[0])).toBe('done')
    expect(syncStatus(feed.current)).toBe('queued')
  })
})
