import type { LaneEvent, PlateLookup, Site, SiteStatus } from '../data-source'

export const RECORDING_VERSION = 1

// Each body is the live wire body unchanged, so a renamed wire field bumps RECORDING_VERSION.
export type RecordedEvent = { at_ms: number; site_id: string } & (
  LaneEvent | { type: 'status'; body: SiteStatus }
)

export interface Recording {
  version: typeof RECORDING_VERSION
  sites: Site[]
  events: RecordedEvent[]
  lookups: Record<string, PlateLookup>
}

const EVENT_TYPES = new Set<RecordedEvent['type']>([
  'decision',
  'resolved',
  'synced',
  'status',
])

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function parseEvent(
  raw: unknown,
  index: number,
  previousAtMs: number,
): RecordedEvent {
  if (
    !isObject(raw) ||
    typeof raw.at_ms !== 'number' ||
    typeof raw.site_id !== 'string'
  ) {
    throw new Error(
      `Recording event ${index} needs a numeric at_ms and a site_id`,
    )
  }
  if (raw.at_ms < previousAtMs) {
    throw new Error(
      `Recording event ${index} has an offset earlier than the event before it`,
    )
  }
  if (!EVENT_TYPES.has(raw.type as RecordedEvent['type'])) {
    throw new Error(
      `Recording event ${index} has an unknown type "${String(raw.type)}"`,
    )
  }
  if (!isObject(raw.body)) {
    throw new Error(`Recording event ${index} carries no body`)
  }
  return raw as unknown as RecordedEvent
}

export function parseRecording(raw: unknown): Recording {
  if (!isObject(raw)) throw new Error('A recording is a JSON object')
  if (raw.version !== RECORDING_VERSION) {
    throw new Error(
      `Recording version ${String(raw.version)} is not supported, this build reads version ${RECORDING_VERSION}`,
    )
  }
  if (
    !Array.isArray(raw.sites) ||
    !Array.isArray(raw.events) ||
    !isObject(raw.lookups)
  ) {
    throw new Error('A recording needs sites, events, and lookups')
  }
  let previousAtMs = 0
  const events = raw.events.map((event, index) => {
    const parsed = parseEvent(event, index, previousAtMs)
    previousAtMs = parsed.at_ms
    return parsed
  })
  return {
    version: RECORDING_VERSION,
    sites: raw.sites as Site[],
    events,
    lookups: raw.lookups as Record<string, PlateLookup>,
  }
}
