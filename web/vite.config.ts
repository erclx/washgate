import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import path from 'path'
import { defineConfig, type ProxyOptions } from 'vite'

import { parseSites, siteAgentPort } from './src/data-source/sites'

const portOffset = Number(process.env.WORKTREE_PORT_OFFSET) || 0
const centralUrl = process.env.CENTRAL_URL ?? 'http://localhost:8080'

const dataSource = process.env.VITE_DATA_SOURCE ?? 'live'
if (dataSource !== 'live' && dataSource !== 'replay') {
  throw new Error(
    `VITE_DATA_SOURCE is "${dataSource}", expected live or replay`,
  )
}

function stripPrefix(prefix: string, target: string): ProxyOptions {
  return {
    target,
    changeOrigin: true,
    rewrite: (url) => url.slice(prefix.length) || '/',
  }
}

// Same-origin paths for Live, so no Go service needs CORS. nginx.conf mirrors these for the compose build.
const apiProxy: Record<string, ProxyOptions> = {
  '/api/central': stripPrefix('/api/central', centralUrl),
  ...Object.fromEntries(
    parseSites(process.env.VITE_SITES).map((site, index) => {
      const prefix = `/api/sites/${site.id}`
      return [
        prefix,
        stripPrefix(prefix, `http://localhost:${siteAgentPort(index)}`),
      ]
    }),
  ),
}

export default defineConfig({
  plugins: [react(), tailwindcss()],
  define: {
    __IS_REPLAY_BUILD__: JSON.stringify(dataSource === 'replay'),
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  base: process.env.VITE_BASE_URL ?? '/',
  server: {
    port: 5173 + portOffset,
    strictPort: true,
    proxy: apiProxy,
  },
  preview: {
    port: 4173 + portOffset,
    strictPort: true,
  },
})
