import type { WuuDesktopApi } from '../../../../../../packages/workbench-ui/src/shared/protocol'

declare global {
  interface Window {
    wuu: WuuDesktopApi
    wuuRenderableFileURL?: (encodedPath: string) => string
  }
}
