// Main window sizing helpers. Kept as a separate module so the math is unit
// testable without spinning up Electron's BrowserWindow in a unit test.

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
