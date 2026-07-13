export function SidePanelToggleIcon({
  side,
  open,
  size = 18
}: {
  side: "left" | "right";
  open: boolean;
  size?: number;
}): JSX.Element {
  const paneX = side === "left" ? 3.2 : 10.4;
  const dividerX = side === "left" ? 8.4 : 9.6;
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
      <rect className="side-panel-toggle-frame" x="1.5" y="1.5" width="15" height="15" rx="3" />
      <path className="side-panel-toggle-divider" d={`M${dividerX} 2.3v13.4`} />
      <rect className="side-panel-toggle-pane" x={paneX} y="3.6" width="4.4" height="10.8" rx="1.2" />
    </svg>
  );
}
