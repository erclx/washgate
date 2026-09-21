import { useEffect, useReducer, useState } from 'react'

import type { SiteStatus } from '@/data-source/data-source'
import { useDataSource } from '@/data-source/data-source-context'

import { EMPTY_LANE_FEED, laneFeedReducer } from '../lane-feed'

export function useSiteLane(siteId: string) {
  const { lane, site } = useDataSource()
  const [feed, dispatch] = useReducer(laneFeedReducer, EMPTY_LANE_FEED)
  const [status, setStatus] = useState<SiteStatus | null>(null)
  const [isAgentDown, setIsAgentDown] = useState(false)

  useEffect(
    () =>
      lane.subscribeToLane(
        siteId,
        (event) => {
          setIsAgentDown(false)
          dispatch(event)
        },
        () => setIsAgentDown(true),
      ),
    [lane, siteId],
  )

  useEffect(
    () =>
      site.subscribeToStatus(
        siteId,
        (nextStatus) => {
          setIsAgentDown(false)
          setStatus(nextStatus)
        },
        () => setIsAgentDown(true),
      ),
    [site, siteId],
  )

  return { feed, status, isAgentDown }
}
