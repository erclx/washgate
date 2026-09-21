import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  buildDecision,
  buildLookup,
  buildStaffDecision,
  buildStatus,
} from '@/test/factories'

import type { RecordedEvent, Recording } from './recording'
import { createReplayDataSource } from './replay-data-source'

const LOOP_GAP_MS = 4000

function buildRecording(events: RecordedEvent[]): Recording {
  return {
    version: 1,
    sites: [{ id: 'site-1', name: 'Site 1' }],
    events,
    lookups: { ABC123: buildLookup() },
  }
}

const staffDecision = buildStaffDecision({ id: 'dec-staff' })
const staffResolution = buildStaffDecision({
  id: 'dec-staff',
  plate: 'MLB482',
  outcome: 'admit',
  reason: 'confirmed_by_staff',
})

function staffRecording() {
  return buildRecording([
    { at_ms: 0, site_id: 'site-1', type: 'decision', body: staffDecision },
    { at_ms: 1000, site_id: 'site-1', type: 'resolved', body: staffResolution },
    {
      at_ms: 1500,
      site_id: 'site-1',
      type: 'decision',
      body: buildDecision({ id: 'dec-after' }),
    },
  ])
}

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('replay lane feed', () => {
  it('delivers each recorded event at its recorded offset', async () => {
    const source = createReplayDataSource({
      recording: buildRecording([
        {
          at_ms: 0,
          site_id: 'site-1',
          type: 'decision',
          body: buildDecision({ id: 'dec-a' }),
        },
        {
          at_ms: 3000,
          site_id: 'site-1',
          type: 'decision',
          body: buildDecision({ id: 'dec-b' }),
        },
      ]),
      photos: {},
    })
    const onEvent = vi.fn()

    source.lane.subscribeToLane('site-1', onEvent, vi.fn())
    await vi.advanceTimersByTimeAsync(2999)
    const idsBeforeOffset = onEvent.mock.calls.map(([event]) => event.body.id)
    await vi.advanceTimersByTimeAsync(1)

    expect(idsBeforeOffset).toEqual(['dec-a'])
    expect(onEvent).toHaveBeenLastCalledWith({
      type: 'decision',
      body: expect.objectContaining({ id: 'dec-b' }),
    })
  })

  it('waits on an open staff decision until the viewer answers it', async () => {
    const source = createReplayDataSource({
      recording: staffRecording(),
      photos: {},
    })
    const onEvent = vi.fn()

    source.lane.subscribeToLane('site-1', onEvent, vi.fn())
    await vi.advanceTimersByTimeAsync(10_000)

    expect(onEvent).toHaveBeenCalledOnce()
  })

  it('enables only the action the recording holds for the staff decision', async () => {
    const source = createReplayDataSource({
      recording: staffRecording(),
      photos: {},
    })

    source.lane.subscribeToLane('site-1', vi.fn(), vi.fn())
    await vi.advanceTimersByTimeAsync(0)

    expect(source.lane.enabledStaffActions('dec-staff')).toEqual(['confirm'])
  })

  it('plays the recorded resolution and resumes once the viewer confirms', async () => {
    const source = createReplayDataSource({
      recording: staffRecording(),
      photos: {},
    })
    const onEvent = vi.fn()

    source.lane.subscribeToLane('site-1', onEvent, vi.fn())
    await vi.advanceTimersByTimeAsync(0)
    await source.lane.confirmPlate('site-1', 'dec-staff', 'MLB482')
    await vi.advanceTimersByTimeAsync(500)

    expect(
      onEvent.mock.calls.map(([event]) => [event.type, event.body.id]),
    ).toEqual([
      ['decision', 'dec-staff'],
      ['resolved', 'dec-staff'],
      ['decision', 'dec-after'],
    ])
  })

  it('refuses a staff action the recording does not hold', async () => {
    const source = createReplayDataSource({
      recording: staffRecording(),
      photos: {},
    })

    source.lane.subscribeToLane('site-1', vi.fn(), vi.fn())
    await vi.advanceTimersByTimeAsync(0)

    await expect(source.lane.sendToPay('site-1', 'dec-staff')).rejects.toThrow(
      /recording/,
    )
  })

  it('starts the recording again after the last event, with fresh decision ids', async () => {
    const source = createReplayDataSource({
      recording: buildRecording([
        {
          at_ms: 0,
          site_id: 'site-1',
          type: 'decision',
          body: buildDecision({ id: 'dec-a' }),
        },
      ]),
      photos: {},
    })
    const onEvent = vi.fn()

    source.lane.subscribeToLane('site-1', onEvent, vi.fn())
    await vi.advanceTimersByTimeAsync(LOOP_GAP_MS)

    expect(onEvent.mock.calls.map(([event]) => event.body.id)).toEqual([
      'dec-a',
      'dec-a~1',
    ])
  })
})

describe('replay clock', () => {
  it('moves recorded times onto the viewer clock, keeping their spacing', async () => {
    vi.setSystemTime(new Date('2026-10-01T09:00:00Z'))
    const source = createReplayDataSource({
      recording: buildRecording([
        {
          at_ms: 0,
          site_id: 'site-1',
          type: 'decision',
          body: buildDecision({ decided_at: '2026-09-21T12:00:00Z' }),
        },
        {
          at_ms: 0,
          site_id: 'site-1',
          type: 'status',
          body: buildStatus({ last_synced_at: '2026-09-21T11:59:58Z' }),
        },
      ]),
      photos: {},
    })
    const onEvent = vi.fn()
    const onStatus = vi.fn()

    source.site.subscribeToStatus('site-1', onStatus, vi.fn())
    source.lane.subscribeToLane('site-1', onEvent, vi.fn())

    expect(onEvent.mock.calls[0][0].body.decided_at).toBe(
      '2026-10-01T09:00:00.000Z',
    )
    expect(onStatus).toHaveBeenLastCalledWith(
      buildStatus({ last_synced_at: '2026-10-01T08:59:58.000Z' }),
    )
  })
})

describe('replay site status', () => {
  it('sends the recorded status for the site as it plays', async () => {
    const offline = buildStatus({ link: 'cut', outbox_depth: 2 })
    const source = createReplayDataSource({
      recording: buildRecording([
        { at_ms: 0, site_id: 'site-1', type: 'status', body: offline },
      ]),
      photos: {},
    })
    const onStatus = vi.fn()

    source.site.subscribeToStatus('site-1', onStatus, vi.fn())
    source.lane.subscribeToLane('site-1', vi.fn(), vi.fn())
    await vi.advanceTimersByTimeAsync(0)

    expect(onStatus).toHaveBeenLastCalledWith(offline)
  })

  it('does not let the viewer switch the link', () => {
    const source = createReplayDataSource({
      recording: staffRecording(),
      photos: {},
    })

    expect(source.site.canSwitchLink()).toBe(false)
  })
})

describe('replay plate lookup', () => {
  it('answers a recorded plate from the recording', async () => {
    const source = createReplayDataSource({
      recording: staffRecording(),
      photos: {},
    })

    const result = await source.lookup.lookUpPlate('abc 123')

    expect(result).toEqual({ isFound: true, lookup: buildLookup() })
  })

  it('answers no match for a plate outside the recording', async () => {
    const source = createReplayDataSource({
      recording: staffRecording(),
      photos: {},
    })

    const result = await source.lookup.lookUpPlate('ZZZ999')

    expect(result).toEqual({ isFound: false, plate: 'ZZZ999' })
  })
})
