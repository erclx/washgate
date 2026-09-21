import type { Site } from './data-source'

const DEFAULT_SITES = 'site-1=Site 1'
const FIRST_SITE_AGENT_PORT = 8081

export function parseSites(raw: string | undefined): Site[] {
  const list = raw?.trim() ? raw : DEFAULT_SITES
  return list.split(',').map((entry) => {
    const [id, name] = entry.split('=').map((part) => part.trim())
    if (!id || !name) {
      throw new Error(
        `VITE_SITES entry "${entry.trim()}" needs the form id=name`,
      )
    }
    return { id, name }
  })
}

// The dev proxy and nginx both assume one site agent per port, counting up from compose's first.
export function siteAgentPort(index: number): number {
  return FIRST_SITE_AGENT_PORT + index
}
