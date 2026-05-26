export function SidePanelToggleIcon({
  side,
  open,
  size = 20
}: {
  side: "left" | "right";
  open: boolean;
  size?: number;
}): JSX.Element {
  const paneX = side === "left" ? 4 : 10.2;
  const dividerX = side === "left" ? 8.5 : 9.5;
  return (
    <svg
      className="side-panel-toggle-icon"
      data-open={open}
      width={size}
      height={size}
      viewBox="0 0 18 18"
      aria-hidden="true"
      focusable="false"
    >
      <rect className="side-panel-toggle-frame" x="2.65" y="3.05" width="12.7" height="11.9" rx="2.4" />
      <path className="side-panel-toggle-divider" d={`M${dividerX} 3.65v10.7`} />
      <rect className="side-panel-toggle-pane" x={paneX} y="4.75" width="3.8" height="8.5" rx="1.1" />
    </svg>
  );
}
