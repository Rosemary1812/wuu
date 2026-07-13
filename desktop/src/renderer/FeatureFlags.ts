/// <reference types="vite/client" />

/**
 * Collaboration (Agents and group chats) is still experimental. Keep it
 * opt-in until its runtime behavior is ready for general release.
 *
 * Vite replaces this value at build time. Use
 * `VITE_ENABLE_COLLABORATION=true npm run dev` for internal testing.
 */
export const ENABLE_COLLABORATION =
  import.meta.env.VITE_ENABLE_COLLABORATION === "true";
