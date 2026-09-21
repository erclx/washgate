import type {
  DataSource,
  LaneDecision,
  LaneEvent,
  SiteStatus,
  StaffAction,
} from '../data-source'
import type { RecordedEvent, Recording } from './recording'

const DEFAULT_LOOP_GAP_MS = 4000
const LOOP_ID_SEPARATOR = '~'

interface ReplayOptions {
  recording: Recording
  photos: Record<string, string>
  loopGapMs?: number
}

interface LaneListener {
  siteId: string
  onEvent: (event: LaneEvent) => void
}

interface StatusListener {
  siteId: string
  onStatus: (status: SiteStatus) => void
}

interface PendingAnswer {
  decisionId: string
  action: StaffAction
  resolutionIndex: number
}

function normalizePlate(plate: string): string {
  return plate.toUpperCase().replace(/[^A-Z0-9]/g, '')
}

function baseId(id: string): string {
  return id.split(LOOP_ID_SEPARATOR)[0]
}

function actionOf(resolution: LaneDecision): StaffAction {
  return resolution.reason === 'sent_to_pay_by_staff'
    ? 'send_to_pay'
    : 'confirm'
}

function findRecordedStartMs(events: RecordedEvent[]): number | null {
  const first = events.find((event) => event.type === 'decision')
  return first?.type === 'decision'
    ? Date.parse(first.body.decided_at) - first.at_ms
    : null
}

export function createReplayDataSource({
  recording,
  photos,
  loopGapMs = DEFAULT_LOOP_GAP_MS,
}: ReplayOptions): DataSource {
  const { events } = recording
  const laneListeners = new Set<LaneListener>()
  const statusListeners = new Set<StatusListener>()
  const lastStatus = new Map<string, SiteStatus>()
  const answeredEarly = new Set<number>()
  let cursor = 0
  let loop = 0
  let lastPlayedAtMs = 0
  let timer: ReturnType<typeof setTimeout> | null = null
  let pending: PendingAnswer | null = null

  // Each pass through the recording relabels its ids, so a looped car never collides with its earlier self.
  const label = (id: string) =>
    loop === 0 ? id : `${id}${LOOP_ID_SEPARATOR}${loop}`

  // Recorded times move onto the viewer's clock, so an age such as the last sync reads as it did when recorded.
  const recordedStartMs = findRecordedStartMs(events)
  const retime = (timestamp: string, atMs: number) =>
    recordedStartMs === null
      ? timestamp
      : new Date(
          Date.parse(timestamp) + Date.now() - (recordedStartMs + atMs),
        ).toISOString()

  function relabel(event: RecordedEvent): RecordedEvent {
    switch (event.type) {
      case 'decision':
      case 'resolved':
        return {
          ...event,
          body: {
            ...event.body,
            id: label(event.body.id),
            wash_id: event.body.wash_id && label(event.body.wash_id),
            decided_at: retime(event.body.decided_at, event.at_ms),
          },
        }
      case 'synced':
        return { ...event, body: { wash_ids: event.body.wash_ids.map(label) } }
      case 'status':
        return { ...event, body: retimeStatus(event.body, event.at_ms) }
    }
  }

  function retimeStatus(status: SiteStatus, atMs: number): SiteStatus {
    return {
      ...status,
      last_synced_at:
        status.last_synced_at && retime(status.last_synced_at, atMs),
    }
  }

  function emit(recorded: RecordedEvent) {
    const event = relabel(recorded)
    if (event.type === 'status') {
      lastStatus.set(event.site_id, event.body)
      statusListeners.forEach((listener) => {
        if (listener.siteId === event.site_id) listener.onStatus(event.body)
      })
      return
    }
    const laneEvent = { type: event.type, body: event.body } as LaneEvent
    laneListeners.forEach((listener) => {
      if (listener.siteId === event.site_id) listener.onEvent(laneEvent)
    })
  }

  function findResolution(decisionIndex: number): number {
    const decision = events[decisionIndex]
    if (decision.type !== 'decision' || decision.body.outcome !== 'staff')
      return -1
    return events.findIndex(
      (event, index) =>
        index > decisionIndex &&
        event.type === 'resolved' &&
        event.body.id === decision.body.id,
    )
  }

  function scheduleNext() {
    while (answeredEarly.has(cursor)) cursor++
    if (cursor >= events.length) {
      timer = setTimeout(() => {
        loop += 1
        cursor = 0
        lastPlayedAtMs = 0
        answeredEarly.clear()
        scheduleNext()
      }, loopGapMs)
      return
    }
    const delayMs = events[cursor].at_ms - lastPlayedAtMs
    if (delayMs <= 0) {
      playNext()
      return
    }
    timer = setTimeout(playNext, delayMs)
  }

  function playNext() {
    timer = null
    const index = cursor
    const event = events[index]
    cursor += 1
    lastPlayedAtMs = event.at_ms
    emit(event)

    const resolutionIndex = findResolution(index)
    if (event.type === 'decision' && resolutionIndex !== -1) {
      const resolution = events[resolutionIndex] as Extract<
        RecordedEvent,
        { type: 'resolved' }
      >
      pending = {
        decisionId: label(event.body.id),
        action: actionOf(resolution.body),
        resolutionIndex,
      }
      return
    }
    scheduleNext()
  }

  function start() {
    if (timer === null && pending === null) scheduleNext()
  }

  function stop() {
    if (timer !== null) clearTimeout(timer)
    timer = null
  }

  async function answer(decisionId: string, action: StaffAction) {
    if (
      pending === null ||
      pending.decisionId !== decisionId ||
      pending.action !== action
    ) {
      throw new Error('The recording holds no such answer for this car')
    }
    const { resolutionIndex } = pending
    pending = null
    answeredEarly.add(resolutionIndex)
    lastPlayedAtMs = events[resolutionIndex].at_ms
    emit(events[resolutionIndex])
    if (laneListeners.size > 0) scheduleNext()
  }

  function firstRecordedStatus(siteId: string): SiteStatus | undefined {
    const event = events.find(
      (candidate) =>
        candidate.type === 'status' && candidate.site_id === siteId,
    )
    return event?.type === 'status'
      ? retimeStatus(event.body, event.at_ms)
      : undefined
  }

  return {
    kind: 'replay',
    lane: {
      subscribeToLane(siteId, onEvent) {
        const listener = { siteId, onEvent }
        laneListeners.add(listener)
        start()
        return () => {
          laneListeners.delete(listener)
          if (laneListeners.size === 0) stop()
        }
      },
      photoUrl: (_siteId, decisionId) => photos[baseId(decisionId)] ?? null,
      confirmPlate: (_siteId, decisionId) => answer(decisionId, 'confirm'),
      sendToPay: (_siteId, decisionId) => answer(decisionId, 'send_to_pay'),
      enabledStaffActions: (decisionId) =>
        pending?.decisionId === decisionId ? [pending.action] : [],
    },
    site: {
      sites: () => recording.sites,
      subscribeToStatus(siteId, onStatus) {
        const listener = { siteId, onStatus }
        statusListeners.add(listener)
        const known = lastStatus.get(siteId) ?? firstRecordedStatus(siteId)
        if (known) onStatus(known)
        return () => {
          statusListeners.delete(listener)
        }
      },
      setLinkCut: async () => {
        throw new Error(
          'The replay plays the recorded link cut and cannot cut the link',
        )
      },
      canSwitchLink: () => false,
    },
    lookup: {
      async lookUpPlate(plate) {
        const lookup = recording.lookups[normalizePlate(plate)]
        return lookup ? { isFound: true, lookup } : { isFound: false, plate }
      },
    },
  }
}
