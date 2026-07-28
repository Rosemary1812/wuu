import { translateCurrent } from "./i18n";
import type { SubagentChipDisplay } from "./AgentHandoff";

// Chips are intentionally non-interactive (role="status" only): clicks pass
// through to whatever row hosts the chip — for a process surface that means
// toggling its fold, which is the desired behavior since the event's context
// lives there. If a chip ever gains its own click target (e.g. jumping to a
// failed subagent), stopPropagation on the chip first.
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

export function SubagentChipList({
  displays,
}: {
  displays: SubagentChipDisplay[];
}): JSX.Element | null {
  if (displays.length === 0) return null;
  const grouped = aggregateSubagentChips(displays);
  return (
    <span className="subagent-chip-list">
      {grouped.map((display, index) => (
        <SubagentChip key={`${index}-${display.label}`} display={display} />
      ))}
    </span>
  );
}

// Collapse runs of adjacent completed chips into one summary chip so the
// host row stays single-line. Every other outcome keeps its name: a failed
// or cancelled chip is exactly the one the user needs to locate, so it must
// never dissolve into a count.
export function aggregateSubagentChips(
  displays: SubagentChipDisplay[],
): SubagentChipDisplay[] {
  if (displays.length < 2) return displays;
  const result: SubagentChipDisplay[] = [];
  let run: SubagentChipDisplay[] = [];
  const flushRun = (): void => {
    if (run.length >= 2) {
      result.push({
        label: translateCurrent("agent.handoff.chip.completedSummary", {
          count: run.length,
        }),
        outcome: "completed",
      });
    } else {
      result.push(...run);
    }
    run = [];
  };
  for (const display of displays) {
    if (display.outcome === "completed") {
      run.push(display);
      continue;
    }
    flushRun();
    result.push(display);
  }
  flushRun();
  return result;
}
