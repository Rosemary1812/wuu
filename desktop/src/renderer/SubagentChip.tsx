import type { SubagentChipDisplay } from "./AgentHandoff";

export function SubagentChip({
  display,
}: {
  display: SubagentChipDisplay;
}): JSX.Element {
  return (
    <span
      className="subagent-chip"
      role="status"
      aria-label={display.label}
    >
      {display.label}
    </span>
  );
}
