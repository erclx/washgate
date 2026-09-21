/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_SITES?: string
}

declare const __IS_REPLAY_BUILD__: boolean

interface ImportMeta {
  readonly env: ImportMetaEnv
}
