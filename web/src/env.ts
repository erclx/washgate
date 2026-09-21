// The data source switch is validated in vite.config.ts and reaches the code as __IS_REPLAY_BUILD__,
// a compile-time constant, so the build can drop the implementation not chosen.
export const SITES_SETTING = import.meta.env.VITE_SITES
