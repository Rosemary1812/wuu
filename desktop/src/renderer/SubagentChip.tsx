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

const CHIP_STATUS_SUFFIXES = [
  "完成了",
  "失败了",
  "已取消",
  "执行中",
  "等待中",
  "已更新",
] as const;

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

export function aggregateSubagentChips(
  displays: SubagentChipDisplay[],
): SubagentChipDisplay[] {
  if (displays.length < 2) return displays;
  const groups = new Map<string, number>();
  for (const display of displays) {
    const suffix = CHIP_STATUS_SUFFIXES.find((candidate) =>
      display.label.endsWith(candidate),
    ) ?? "已更新";
    groups.set(suffix, (groups.get(suffix) ?? 0) + 1);
  }
  return Array.from(groups, ([suffix, count]) => ({
    label: `${count} 个 subagent ${suffix}`,
    shimmer: true,
  }));
}
