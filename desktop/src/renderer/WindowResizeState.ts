export const WINDOW_RESIZING_CLASS = "window-resizing";
export const LAYOUT_MOTION_CLASS = "layout-motion-active";
export const WINDOW_RESIZE_SETTLE_DELAY_MS = 160;

export function isWindowResizing(): boolean {
  if (typeof document === "undefined") {
    return false;
  }
  const root = document.documentElement;
  return (
    root.classList.contains(WINDOW_RESIZING_CLASS) ||
    root.classList.contains(LAYOUT_MOTION_CLASS)
  );
}

export type WindowResizeSettleScheduler = {
  schedule: () => void;
  cancel: () => void;
};

export function createWindowResizeSettleScheduler(
  callback: () => void,
): WindowResizeSettleScheduler {
  let timer: number | undefined;

  const cancel = (): void => {
    if (timer !== undefined) {
      window.clearTimeout(timer);
      timer = undefined;
    }
  };

  const schedule = (): void => {
    cancel();
    timer = window.setTimeout(() => {
      timer = undefined;
      if (isWindowResizing()) {
        schedule();
        return;
      }
      callback();
    }, WINDOW_RESIZE_SETTLE_DELAY_MS);
  };

  return { schedule, cancel };
}
