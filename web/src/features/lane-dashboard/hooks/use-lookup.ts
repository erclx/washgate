import { useCallback, useRef, useState } from 'react'

import { useDataSource } from '@/data-source/data-source-context'

import type { LookupState } from '../components/lookup-panel'

export function useLookup() {
  const { lookup } = useDataSource()
  const [lookupState, setLookupState] = useState<LookupState | null>(null)
  const latestRequest = useRef(0)

  const search = useCallback(
    async (plate: string) => {
      const request = ++latestRequest.current
      setLookupState({ status: 'loading', plate })
      try {
        const result = await lookup.lookUpPlate(plate)
        if (request !== latestRequest.current) return
        setLookupState(
          result.isFound
            ? { status: 'found', lookup: result.lookup }
            : { status: 'none', plate: result.plate },
        )
      } catch {
        if (request === latestRequest.current)
          setLookupState({ status: 'error' })
      }
    },
    [lookup],
  )

  const close = useCallback(() => {
    latestRequest.current += 1
    setLookupState(null)
  }, [])

  return { lookupState, search, close }
}
