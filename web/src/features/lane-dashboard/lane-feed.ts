import type { LaneDecision, LaneEvent } from '@/data-source/data-source'

const EARLIER_CARS_LIMIT = 50

export interface LaneFeed {
  readonly current: LaneDecision | null
  readonly queued: readonly LaneDecision[]
  readonly earlier: readonly LaneDecision[]
}

export const EMPTY_LANE_FEED: LaneFeed = {
  current: null,
  queued: [],
  earlier: [],
}

function isOpenStaff(decision: LaneDecision | null): boolean {
  return decision?.outcome === 'staff'
}

function replaceById(
  decisions: readonly LaneDecision[],
  next: LaneDecision,
): LaneDecision[] {
  return decisions.map((decision) =>
    decision.id === next.id ? next : decision,
  )
}

function holds(feed: LaneFeed, id: string): boolean {
  return (
    feed.current?.id === id ||
    feed.queued.some((decision) => decision.id === id) ||
    feed.earlier.some((decision) => decision.id === id)
  )
}

function replaceHeld(feed: LaneFeed, next: LaneDecision): LaneFeed {
  return {
    current: feed.current?.id === next.id ? next : feed.current,
    queued: replaceById(feed.queued, next),
    earlier: replaceById(feed.earlier, next),
  }
}

function advance(
  current: LaneDecision | null,
  earlier: readonly LaneDecision[],
  next: LaneDecision,
) {
  return {
    current: next,
    earlier: (current ? [current, ...earlier] : earlier).slice(
      0,
      EARLIER_CARS_LIMIT,
    ),
  }
}

function drainQueue(feed: LaneFeed): LaneFeed {
  let { current, earlier } = feed
  let drained = 0
  while (drained < feed.queued.length && !isOpenStaff(current)) {
    ;({ current, earlier } = advance(current, earlier, feed.queued[drained]))
    drained += 1
  }
  return { current, queued: feed.queued.slice(drained), earlier }
}

function markSynced(
  decision: LaneDecision,
  washIds: ReadonlySet<string>,
): LaneDecision {
  if (decision.wash_id === null || !washIds.has(decision.wash_id))
    return decision
  return {
    ...decision,
    trace: decision.trace.map((step) =>
      step.step === 'sync_to_hq' && step.status === 'queued'
        ? { ...step, status: 'done' }
        : step,
    ),
  }
}

export function laneFeedReducer(feed: LaneFeed, event: LaneEvent): LaneFeed {
  switch (event.type) {
    case 'decision': {
      if (holds(feed, event.body.id)) return replaceHeld(feed, event.body)
      if (isOpenStaff(feed.current))
        return { ...feed, queued: [...feed.queued, event.body] }
      return { ...feed, ...advance(feed.current, feed.earlier, event.body) }
    }
    case 'resolved': {
      const wasCurrent = feed.current?.id === event.body.id
      const replaced = replaceHeld(feed, event.body)
      return wasCurrent ? drainQueue(replaced) : replaced
    }
    case 'synced': {
      const washIds = new Set(event.body.wash_ids)
      return {
        current: feed.current && markSynced(feed.current, washIds),
        queued: feed.queued.map((decision) => markSynced(decision, washIds)),
        earlier: feed.earlier.map((decision) => markSynced(decision, washIds)),
      }
    }
  }
}
