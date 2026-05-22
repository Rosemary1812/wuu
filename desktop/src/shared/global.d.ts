import type { WuuDesktopApi } from "./protocol";

declare global {
  interface Window {
    wuu: WuuDesktopApi;
  }
}

export {};
