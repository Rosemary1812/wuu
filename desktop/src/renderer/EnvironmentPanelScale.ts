const FULL_SIZE_PANEL_CONTAINER_WIDTH = 560;
const MAXIMUM_ENVIRONMENT_PANEL_SCALE = 0.8;
const MINIMUM_ENVIRONMENT_PANEL_SCALE = 0.576;

export function environmentPanelScaleForWidth(containerWidth: number): number {
  const scale = Math.min(
    MAXIMUM_ENVIRONMENT_PANEL_SCALE,
    Math.max(
      MINIMUM_ENVIRONMENT_PANEL_SCALE,
      (containerWidth / FULL_SIZE_PANEL_CONTAINER_WIDTH) *
        MAXIMUM_ENVIRONMENT_PANEL_SCALE,
    ),
  );
  return Math.round(scale * 1_000) / 1_000;
}
