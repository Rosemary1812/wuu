// Main window sizing helpers. Kept as a separate module so the math is unit
// testable without spinning up Electron's BrowserWindow in a unit test.

import {
  readDesktopSettings,
  writeDesktopSettings,
} from "./desktopSettings";

export const MAIN_WINDOW_DEFAULT_WIDTH_RATIO = 0.40;
export const MAIN_WINDOW_DEFAULT_HEIGHT_RATIO = 0.60;
export const MAIN_WINDOW_MIN_WIDTH = 880;
export const MAIN_WINDOW_MIN_HEIGHT = 560;
export const MAIN_WINDOW_MAX_WIDTH = 1600;
export const MAIN_WINDOW_MAX_HEIGHT = 1100;

export type WorkArea = {
  width: number;
  height: number;
};

export type MainWindowSize = {
  width: number;
  height: number;
};

// Default main window size: roughly a quarter of the screen area, with a
// slightly taller-than-wide aspect (40% × 60%) so the chat / composer area
// has vertical room for a typical agent conversation.
//
//  - Width is clamped between [MIN_WIDTH, MAX_WIDTH] so small laptops do not
//    get a useless narrow window and 4K displays do not get a window that
//    spans half the screen.
//  - The same clamp is applied to height for symmetry, with the cap tuned
//    below the width cap so the aspect stays vertical even on huge displays.
export function computeDefaultMainWindowBounds(workArea: WorkArea): MainWindowSize {
  const targetWidth = MAIN_WINDOW_DEFAULT_WIDTH_RATIO * workArea.width;
  const targetHeight = MAIN_WINDOW_DEFAULT_HEIGHT_RATIO * workArea.height;
  const width = Math.min(
    MAIN_WINDOW_MAX_WIDTH,
    Math.max(MAIN_WINDOW_MIN_WIDTH, Math.round(targetWidth)),
  );
  const height = Math.min(
    MAIN_WINDOW_MAX_HEIGHT,
    Math.max(MAIN_WINDOW_MIN_HEIGHT, Math.round(targetHeight)),
  );
  return { width, height };
}

export type WindowBounds = {
  x: number;
  y: number;
  width: number;
  height: number;
};

export type DisplayLike = {
  workArea: { x: number; y: number; width: number; height: number };
};

// Loads the last-known main window bounds from desktop-settings.json. Returns
// null when there is no saved value, the value is malformed, or the saved
// center point does not fall inside any currently connected display — so
// callers can fall back to computeDefaultMainWindowBounds with no extra
// branching. The off-screen check guards the multi-monitor unplug case where
// the saved bounds point at a display that is no longer attached, which
// would otherwise leave the user with a window they cannot see on relaunch.
export function loadMainWindowBounds(
  displays: DisplayLike[],
  options: { filePath?: string } = {},
): WindowBounds | null {
  const saved = readDesktopSettings(options.filePath).main_window_bounds;
  if (!saved) return null;
  if (
    !Number.isFinite(saved.x) ||
    !Number.isFinite(saved.y) ||
    !Number.isFinite(saved.width) ||
    !Number.isFinite(saved.height)
  ) {
    return null;
  }
  if (saved.width <= 0 || saved.height <= 0) {
    return null;
  }
  const centerX = saved.x + saved.width / 2;
  const centerY = saved.y + saved.height / 2;
  const onScreen = displays.some((d) => {
    const wa = d.workArea;
    return (
      centerX >= wa.x &&
      centerX < wa.x + wa.width &&
      centerY >= wa.y &&
      centerY < wa.y + wa.height
    );
  });
  if (!onScreen) return null;
  return { x: saved.x, y: saved.y, width: saved.width, height: saved.height };
}

// Persists the current main window bounds, preserving other desktop settings.
// Called both from the resize-end debounce and from the close event so the
// most recent values win.
export function saveMainWindowBounds(
  bounds: WindowBounds,
  options: { filePath?: string } = {},
): void {
  const settings = readDesktopSettings(options.filePath);
  writeDesktopSettings(
    { ...settings, main_window_bounds: { ...bounds } },
    options.filePath,
  );
}
