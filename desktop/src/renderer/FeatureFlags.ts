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
 * Collaboration (kanban board, convert-to-task, participant memory) is
 * temporarily hidden from the frontend while the product semantics are being
 * redesigned. The backend RPCs remain in place; only the UI surface is gated.
 *
 * Use `VITE_ENABLE_COLLABORATION=true npm run dev` to re-enable internally.
 */
export const ENABLE_COLLABORATION =
  import.meta.env.VITE_ENABLE_COLLABORATION === "true";

/**
 * Ultra mode is hidden while its multi-agent flow is still being stabilized.
 * Keep the backend support in place, but require an explicit frontend opt-in
 * before exposing the composer control.
 *
 * Use `VITE_ENABLE_ULTRA_MODE=true npm run dev` for internal testing.
 */
export const ENABLE_ULTRA_MODE =
  import.meta.env.VITE_ENABLE_ULTRA_MODE === "true";
