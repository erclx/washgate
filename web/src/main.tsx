import './index.css'

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { App } from '@/app'
import type { DataSource } from '@/data-source/data-source'
import { DataSourceContext } from '@/data-source/data-source-context'
import { SITES_SETTING } from '@/env'

// __IS_REPLAY_BUILD__ is replaced with a literal at build, so the implementation not chosen never reaches the bundle.
async function createDataSource(): Promise<DataSource> {
  if (__IS_REPLAY_BUILD__) {
    const { createDemoReplayDataSource } =
      await import('@/data-source/replay/demo-session')
    return createDemoReplayDataSource()
  }
  const [{ createLiveDataSource }, { parseSites }] = await Promise.all([
    import('@/data-source/live/live-data-source'),
    import('@/data-source/sites'),
  ])
  return createLiveDataSource({ sites: parseSites(SITES_SETTING) })
}

const root = document.getElementById('root')
if (root === null) throw new Error('index.html carries no #root element')

void createDataSource().then((source) => {
  createRoot(root).render(
    <StrictMode>
      <DataSourceContext value={source}>
        <App />
      </DataSourceContext>
    </StrictMode>,
  )
})
