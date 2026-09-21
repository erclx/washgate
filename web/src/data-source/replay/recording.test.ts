import { describe, expect, it } from 'vitest'

import { buildDecision } from '@/test/factories'

import demoSession from './fixtures/demo-session.json'
import { parseRecording } from './recording'

function buildRawRecording(overrides: Record<string, unknown> = {}) {
  return {
    version: 1,
    sites: [{ id: 'site-1', name: 'Site 1' }],
    events: [
      { at_ms: 0, site_id: 'site-1', type: 'decision', body: buildDecision() },
    ],
    lookups: {},
    ...overrides,
  }
}

describe('parseRecording', () => {
  it('accepts a version 1 recording and keeps its events in order', () => {
    const recording = parseRecording(buildRawRecording())

    expect(recording.events[0]).toMatchObject({ at_ms: 0, type: 'decision' })
  })

  it('accepts the demo session the replay build ships', () => {
    expect(() => parseRecording(demoSession)).not.toThrow()
  })

  it('rejects a recording of an unknown version rather than half-parsing it', () => {
    expect(() => parseRecording(buildRawRecording({ version: 2 }))).toThrow(
      /version 2/,
    )
  })

  it('rejects an event of an unknown type', () => {
    const raw = buildRawRecording({
      events: [{ at_ms: 0, site_id: 'site-1', type: 'reboot', body: {} }],
    })

    expect(() => parseRecording(raw)).toThrow(/reboot/)
  })

  it('rejects events whose offsets run backwards', () => {
    const raw = buildRawRecording({
      events: [
        {
          at_ms: 500,
          site_id: 'site-1',
          type: 'decision',
          body: buildDecision(),
        },
        {
          at_ms: 100,
          site_id: 'site-1',
          type: 'decision',
          body: buildDecision(),
        },
      ],
    })

    expect(() => parseRecording(raw)).toThrow(/offset/)
  })
})
