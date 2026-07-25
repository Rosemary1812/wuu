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

/**
 * Group chat is hidden while named-agent collaboration is still maturing.
 * Keep the channel implementation available for internal testing without
 * exposing its navigation entry or background mention polling to users.
 *
 * Use `VITE_ENABLE_GROUP_CHAT=true npm run dev` for internal testing.
 */
export const ENABLE_GROUP_CHAT =
  import.meta.env.VITE_ENABLE_GROUP_CHAT === "true";

/**
 * Voice input and its optional BYOK text polish are hidden while the native
 * recognition flow and polish experience are still being stabilized.
 *
 * Use `VITE_ENABLE_VOICE_INPUT=true npm run dev` for internal testing.
 */
export const ENABLE_VOICE_INPUT =
  import.meta.env.VITE_ENABLE_VOICE_INPUT === "true";
