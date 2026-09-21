import type {
  DataSource,
  LaneEvent,
  LookupResult,
  PlateLookup,
  Site,
  SiteStatus,
} from '../data-source'
import { UnreachableError } from '../data-source'

const REQUEST_TIMEOUT_MS = 5000
const LANE_EVENT_TYPES: LaneEvent['type'][] = ['decision', 'resolved', 'synced']

interface LiveOptions {
  sites: Site[]
  fetch?: typeof fetch
  EventSource?: typeof EventSource
  statusIntervalMs?: number
  apiRoot?: string
}

export function createLiveDataSource({
  sites,
  fetch: fetchImpl = globalThis.fetch.bind(globalThis),
  EventSource: EventSourceImpl = globalThis.EventSource,
  statusIntervalMs = 2000,
  apiRoot = '/api',
}: LiveOptions): DataSource {
  const sitePath = (siteId: string, path: string) =>
    `${apiRoot}/sites/${encodeURIComponent(siteId)}${path}`

  async function send(url: string, target: string, init: RequestInit = {}) {
    const controller = new AbortController()
    const timeout = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)
    try {
      return await fetchImpl(url, { ...init, signal: controller.signal })
    } catch (error) {
      throw new UnreachableError(target, { cause: error })
    } finally {
      clearTimeout(timeout)
    }
  }

  async function sendToSite(
    siteId: string,
    path: string,
    method: string,
    body?: unknown,
  ) {
    const response = await send(
      sitePath(siteId, path),
      `the site agent for ${siteId}`,
      {
        method,
        headers:
          body === undefined
            ? undefined
            : { 'Content-Type': 'application/json' },
        body: body === undefined ? undefined : JSON.stringify(body),
      },
    )
    if (!response.ok) {
      throw new Error(
        `The site agent answered ${response.status} to ${method} ${path}`,
      )
    }
  }

  return {
    kind: 'live',
    lane: {
      subscribeToLane(siteId, onEvent, onUnreachable) {
        const stream = new EventSourceImpl(sitePath(siteId, '/lane/events'))
        for (const type of LANE_EVENT_TYPES) {
          stream.addEventListener(type, (event: MessageEvent<string>) => {
            onEvent({ type, body: JSON.parse(event.data) } as LaneEvent)
          })
        }
        stream.onerror = () => onUnreachable()
        return () => stream.close()
      },
      photoUrl: (siteId, decisionId) =>
        sitePath(siteId, `/lane/photos/${encodeURIComponent(decisionId)}`),
      confirmPlate: (siteId, decisionId, plate) =>
        sendToSite(
          siteId,
          `/lane/decisions/${encodeURIComponent(decisionId)}/confirm`,
          'POST',
          {
            plate,
          },
        ),
      sendToPay: (siteId, decisionId) =>
        sendToSite(
          siteId,
          `/lane/decisions/${encodeURIComponent(decisionId)}/send-to-pay`,
          'POST',
        ),
      enabledStaffActions: () => ['confirm', 'send_to_pay'],
    },
    site: {
      sites: () => sites,
      subscribeToStatus(siteId, onStatus, onUnreachable) {
        let isActive = true
        let pollsStarted = 0
        let newestAnswered = 0
        // Polls overlap when the agent answers slower than the interval, so only a newer poll may report.
        const isNewest = (poll: number) => isActive && poll > newestAnswered
        async function poll() {
          const thisPoll = ++pollsStarted
          try {
            const response = await send(
              sitePath(siteId, '/status'),
              `the site agent for ${siteId}`,
            )
            if (!response.ok)
              throw new UnreachableError(`the site agent for ${siteId}`)
            const status = (await response.json()) as SiteStatus
            if (!isNewest(thisPoll)) return
            newestAnswered = thisPoll
            onStatus(status)
          } catch {
            if (!isNewest(thisPoll)) return
            newestAnswered = thisPoll
            onUnreachable()
          }
        }
        void poll()
        const interval = setInterval(() => void poll(), statusIntervalMs)
        return () => {
          isActive = false
          clearInterval(interval)
        }
      },
      setLinkCut: (siteId, isCut) =>
        sendToSite(siteId, '/site/link', 'PUT', { cut: isCut }),
      canSwitchLink: () => true,
    },
    lookup: {
      async lookUpPlate(plate): Promise<LookupResult> {
        const response = await send(
          `${apiRoot}/central/plates/${encodeURIComponent(plate)}`,
          'head office',
        )
        if (response.status === 404) return { isFound: false, plate }
        if (!response.ok) {
          throw new Error(
            `Head office answered ${response.status} to the plate lookup`,
          )
        }
        return { isFound: true, lookup: (await response.json()) as PlateLookup }
      },
    },
  }
}
