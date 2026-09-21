import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { UnreachableError } from '@/data-source/data-source'
import { buildDecision, buildStatus } from '@/test/factories'

import { createLiveDataSource } from './live-data-source'

class FakeEventSource {
  static instances: FakeEventSource[] = []
  readonly url: string
  onerror: (() => void) | null = null
  isClosed = false
  private listeners = new Map<string, ((event: MessageEvent) => void)[]>()

  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }

  addEventListener(type: string, listener: (event: MessageEvent) => void) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener])
  }

  close() {
    this.isClosed = true
  }

  emit(type: string, body: unknown) {
    const event = new MessageEvent(type, { data: JSON.stringify(body) })
    this.listeners.get(type)?.forEach((listener) => listener(event))
  }
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function createSource(fetchImpl: typeof fetch) {
  return createLiveDataSource({
    sites: [{ id: 'site-1', name: 'Site 1' }],
    fetch: fetchImpl,
    EventSource: FakeEventSource as unknown as typeof EventSource,
    statusIntervalMs: 2000,
  })
}

beforeEach(() => {
  FakeEventSource.instances = []
})

afterEach(() => {
  vi.useRealTimers()
})

describe('live lane feed', () => {
  it('passes a decision event from the site agent stream to the subscriber', () => {
    const source = createSource(vi.fn())
    const onEvent = vi.fn()
    const decision = buildDecision()

    source.lane.subscribeToLane('site-1', onEvent, vi.fn())
    FakeEventSource.instances[0].emit('decision', decision)

    expect(FakeEventSource.instances[0].url).toBe(
      '/api/sites/site-1/lane/events',
    )
    expect(onEvent).toHaveBeenCalledWith({ type: 'decision', body: decision })
  })

  it('reports the site agent unreachable when the stream errors', () => {
    const source = createSource(vi.fn())
    const onUnreachable = vi.fn()

    source.lane.subscribeToLane('site-1', vi.fn(), onUnreachable)
    FakeEventSource.instances[0].onerror?.()

    expect(onUnreachable).toHaveBeenCalledOnce()
  })

  it('closes the stream on unsubscribe', () => {
    const source = createSource(vi.fn())

    const unsubscribe = source.lane.subscribeToLane('site-1', vi.fn(), vi.fn())
    unsubscribe()

    expect(FakeEventSource.instances[0].isClosed).toBe(true)
  })
})

describe('live site status', () => {
  it('polls the site status every two seconds', async () => {
    vi.useFakeTimers()
    const fetchImpl = vi.fn<typeof fetch>(async () =>
      jsonResponse(buildStatus()),
    )
    const source = createSource(fetchImpl)
    const onStatus = vi.fn()

    source.site.subscribeToStatus('site-1', onStatus, vi.fn())
    await vi.advanceTimersByTimeAsync(2000)

    expect(fetchImpl).toHaveBeenCalledTimes(2)
    expect(fetchImpl.mock.calls[0][0]).toBe('/api/sites/site-1/status')
    expect(onStatus).toHaveBeenCalledWith(buildStatus())
  })

  it('reports unreachable when a status poll fails', async () => {
    vi.useFakeTimers()
    const fetchImpl = vi.fn(async () => {
      throw new TypeError('Failed to fetch')
    })
    const source = createSource(fetchImpl)
    const onUnreachable = vi.fn()

    source.site.subscribeToStatus('site-1', vi.fn(), onUnreachable)
    await vi.advanceTimersByTimeAsync(0)

    expect(onUnreachable).toHaveBeenCalledOnce()
  })

  it('drops a slow poll that answers after a newer one', async () => {
    vi.useFakeTimers()
    const older = buildStatus({ outbox_depth: 5 })
    const newer = buildStatus({ outbox_depth: 0 })
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockImplementationOnce(
        () =>
          new Promise((resolve) =>
            setTimeout(() => resolve(jsonResponse(older)), 3000),
          ),
      )
      .mockImplementationOnce(async () => jsonResponse(newer))
    const source = createSource(fetchImpl)
    const onStatus = vi.fn()

    source.site.subscribeToStatus('site-1', onStatus, vi.fn())
    await vi.advanceTimersByTimeAsync(3000)

    expect(onStatus.mock.calls.map(([status]) => status)).toEqual([newer])
  })

  it('puts the cut state to the link route', async () => {
    const fetchImpl = vi.fn(async () => new Response(null, { status: 204 }))
    const source = createSource(fetchImpl)

    await source.site.setLinkCut('site-1', true)

    expect(fetchImpl).toHaveBeenCalledWith(
      '/api/sites/site-1/site/link',
      expect.objectContaining({ method: 'PUT', body: '{"cut":true}' }),
    )
  })
})

describe('live staff actions', () => {
  it('posts the corrected plate to the confirm route', async () => {
    const fetchImpl = vi.fn(async () => new Response(null, { status: 204 }))
    const source = createSource(fetchImpl)

    await source.lane.confirmPlate('site-1', 'dec-9', 'EEK828')

    expect(fetchImpl).toHaveBeenCalledWith(
      '/api/sites/site-1/lane/decisions/dec-9/confirm',
      expect.objectContaining({ method: 'POST', body: '{"plate":"EEK828"}' }),
    )
  })

  it('posts to the send-to-pay route', async () => {
    const fetchImpl = vi.fn(async () => new Response(null, { status: 204 }))
    const source = createSource(fetchImpl)

    await source.lane.sendToPay('site-1', 'dec-9')

    expect(fetchImpl).toHaveBeenCalledWith(
      '/api/sites/site-1/lane/decisions/dec-9/send-to-pay',
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('throws an unreachable error when the site agent cannot be reached', async () => {
    const fetchImpl = vi.fn(async () => {
      throw new TypeError('Failed to fetch')
    })
    const source = createSource(fetchImpl)

    await expect(
      source.lane.sendToPay('site-1', 'dec-9'),
    ).rejects.toBeInstanceOf(UnreachableError)
  })
})

describe('live plate lookup', () => {
  it('answers no match when central answers 404', async () => {
    const fetchImpl = vi.fn<typeof fetch>(
      async () => new Response(null, { status: 404 }),
    )
    const source = createSource(fetchImpl)

    const result = await source.lookup.lookUpPlate('ABC123')

    expect(fetchImpl.mock.calls[0][0]).toBe('/api/central/plates/ABC123')
    expect(result).toEqual({ isFound: false, plate: 'ABC123' })
  })
})
