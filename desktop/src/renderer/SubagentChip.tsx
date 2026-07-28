import type { SubagentChipDisplay } from "./AgentHandoff";

export function SubagentChip({
  display,
}: {
  display: SubagentChipDisplay;
}): JSX.Element {
  return (
    <span
      className={`subagent-chip${display.shimmer ? " subagent-chip-shimmer" : ""}`}
      role="status"
      aria-label={display.label}
    >
      {display.label}
    </span>
  );
}
