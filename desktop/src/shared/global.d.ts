import type { WuuDesktopApi } from "../../../browser/agent/packages/workbench-ui/src/shared/protocol";

declare global {
  interface Window {
    wuu: WuuDesktopApi;
    wuuRenderableFileURL?: (encodedPath: string) => string;
  }
}

export {};
