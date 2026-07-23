/// <reference types="vite/client" />

/**
 * Remote control is still experimental. Keep its desktop settings entry
 * opt-in while the mobile pairing experience is under development.
 *
 * Vite replaces this value at build time. Use
 * `VITE_ENABLE_REMOTE_CONTROL=true npm run dev` for internal testing.
 */
export const ENABLE_REMOTE_CONTROL =
  import.meta.env.VITE_ENABLE_REMOTE_CONTROL === "true";

/**
 * Ultra mode is hidden while its multi-agent flow is still being stabilized.
 * Keep the backend support in place, but require an explicit frontend opt-in
 * before exposing the composer control.
 *
 * Use `VITE_ENABLE_ULTRA_MODE=true npm run dev` for internal testing.
 */
export const ENABLE_ULTRA_MODE =
  import.meta.env.VITE_ENABLE_ULTRA_MODE === "true";
