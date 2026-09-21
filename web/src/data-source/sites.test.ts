import { describe, expect, it } from 'vitest'

import { parseSites, siteAgentPort } from './sites'

describe('parseSites', () => {
  it('reads each comma-separated id=name pair in order', () => {
    expect(parseSites('site-1=Site 1, site-2=Site 2')).toEqual([
      { id: 'site-1', name: 'Site 1' },
      { id: 'site-2', name: 'Site 2' },
    ])
  })

  it('falls back to the one compose site when the list is empty', () => {
    expect(parseSites(undefined)).toEqual([{ id: 'site-1', name: 'Site 1' }])
  })

  it('rejects an entry with no name', () => {
    expect(() => parseSites('site-1')).toThrow(/site-1/)
  })
})

describe('siteAgentPort', () => {
  it('gives the first site the compose site agent port and counts up', () => {
    expect([siteAgentPort(0), siteAgentPort(1)]).toEqual([8081, 8082])
  })
})
