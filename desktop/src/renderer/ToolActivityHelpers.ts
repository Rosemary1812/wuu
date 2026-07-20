import { formatMessageFlowCommand } from "./message-flow-display";
import type { ThreadItem } from "../shared/protocol";
import { userFacingErrorForMessage } from "./UserFacingErrors";
import { translateCurrent as t } from "./i18n";

export type ToolActivityKind =
  | "edit"
  | "create"
  | "search"
  | "read"
  | "list"
  | "command"
  | "agent"
  | "plan"
  | "interaction"
  | "schedule"
  | "browser"
  | "skill"
  | "context"
  | "unknown";

export type ToolActivitySummary = {
  kind: ToolActivityKind;
  text: string;
  fileName?: string;
  additions: number;
  deletions: number;
  running: boolean;
  failed: boolean;
};

type DiffStats = {
  additions: number;
  deletions: number;
  newFile: boolean;
};

export type JsonRecord = Record<string, unknown>;

export type ToolActivitySectionStatus = "running" | "completed" | "failed";

export type ToolActivityCommand = {
  text: string;
  status: ToolActivitySectionStatus;
};

export type ToolActivitySection = {
  id: string;
  kind: ToolActivityKind;
  title: string;
  subtitle?: string;
  detail?: string;
  status: ToolActivitySectionStatus;
  commands: ToolActivityCommand[];
  error?: string;
};

export type ToolActivityProcessSegment = {
  id: string;
  kind: ToolActivityKind;
  status: ToolActivitySectionStatus;
  text?: string;
  countPrefix?: string;
  count?: number;
  countSuffix?: string;
  error?: string;
};

export function buildToolActivitySections(items: ThreadItem[]): ToolActivitySection[] {
  const groups = new Map<string, ThreadItem[]>();
  for (const item of items) {
    const key = toolActivitySectionKey(item);
    groups.set(key, [...(groups.get(key) ?? []), item]);
  }
  return Array.from(groups.entries()).map(([key, grouped]) =>
    toolActivitySectionFromItems(key, grouped),
  );
}

export function buildToolActivityProcessSegments(
  items: ThreadItem[],
): ToolActivityProcessSegment[] {
  const groups = new Map<string, ThreadItem[]>();
  for (const item of items) {
    const key = toolActivitySectionKey(item);
    groups.set(key, [...(groups.get(key) ?? []), item]);
  }
  return Array.from(groups.entries()).map(([key, grouped]) =>
    toolActivityProcessSegmentFromItems(key, grouped),
  );
}

export function activitySummaryText(
  sections: ToolActivitySection[],
  fallback: ToolActivitySummary,
): string {
  if (sections.length === 0) {
    return fallback.text;
  }
  const fragments = uniqueStrings(
    sections.map((section) => sectionSummaryText(section)).filter(Boolean),
  );
  if (fragments.length === 0) {
    return fallback.text;
  }
  const summary = fragments.join(t("toolActivity.listSeparator"));
  return fallback.failed
    ? t("toolActivity.incompleteSummary", { summary })
    : summary;
}

function sectionSummaryText(section: ToolActivitySection): string {
  return section.title;
}

function toolCommands(items: ThreadItem[]): ToolActivityCommand[] {
  // readableToolActivityCommand returns "" until args (or result) actually
  // parses, so a tool call that's mid-stream shows up with no entry yet.
  // Filter those out so we don't render blank lines inside a section.
  return items
    .map((item) => ({
      text: readableToolActivityCommand(item),
      status: itemToolStatus(item),
    }))
    .filter((command) => command.text.trim() !== "");
}

function itemToolStatus(item: ThreadItem): ToolActivitySectionStatus {
  if (item.status === "failed" || item.error) {
    return "failed";
  }
  if ((item.status ?? "in_progress") === "in_progress") {
    return "running";
  }
  return "completed";
}

function rawToolCommand(name: string, args: string | undefined): string {
  const trimmed = args?.trim();
  if (!trimmed) {
    return name || "tool";
  }
  let input: unknown = trimmed;
  try {
    input = JSON.parse(trimmed);
  } catch {
    input = trimmed;
  }
  return formatMessageFlowCommand({ name, input });
}

export function readableToolActivityCommand(
  item: Pick<ThreadItem, "name" | "arguments" | "result" | "result_detail" | "display">,
): string {
  return appendCapabilitySuffix(
    readableToolActivityCommandInner(item),
    item.display?.capability,
  );
}

function readableToolActivityCommandInner(
  item: Pick<ThreadItem, "name" | "arguments" | "result" | "result_detail" | "display">,
): string {
  // Wait until args (or result) actually parses before returning a title.
  // The backend ships a preformatted `item.display.text` ("查看项目目录",
  // "读取 文件") with item/started that lacks the path the args delta will
  // eventually reveal, and the per-tool fallbacks ("读取 文件", "运行命令")
  // do the same. Honoring either on first render and then again once args
  // parse produces a placeholder → real two-step flicker, so we drop both
  // paths and only render once args (or result) is parseable.
  const args = parseJSONRecord(item.arguments);
  const result = richToolResultRecord(item) ?? parseJSONRecord(item.result);
  const name = (item.name ?? "").trim();

  if (isMCPToolName(name)) {
    return rawToolCommand(name, item.arguments);
  }

  // No parseable args and no result yet — the tool call just started.
  // Don't render anything; the next item/toolCall/delta will reveal it.
  if (!args && !result) {
    return "";
  }
  const path =
    stringValue(result, "path") ??
    stringValue(args, "path") ??
    stringValue(args, "file");
  const command =
    stringValue(result, "command") ?? stringValue(args, "command") ?? "";
  const action = stringValue(result, "action") ?? stringValue(args, "action") ?? "";
  const pattern =
    stringValue(args, "pattern") ??
    stringValue(args, "query") ??
    stringValue(args, "q");
  const capability = item.display?.capability?.trim();

  if (capability === "command.background") {
    return readableBackgroundCommandLabel(action, command);
  }
  if (capability === "command.bash" && command) {
    return t("toolActivity.runTarget", { target: truncateText(command, 100) });
  }
  if (capability === "file.edit" && path) {
    return t("toolActivity.updateTarget", { target: formatPathTarget(path, t("toolActivity.file")) });
  }

  switch (name) {
    case "read_file":
      return t("toolActivity.readTarget", { target: formatPathTarget(path, t("toolActivity.file")) });
    case "list_files":
      return path && path !== "."
        ? t("toolActivity.viewTarget", { target: formatDirectoryTarget(path) })
        : t("toolActivity.viewProjectDirectory");
    case "grep":
    case "glob":
      return path && path !== "."
        ? t("toolActivity.searchTargetInScope", {
            target: formatSearchTarget(pattern),
            scope: formatPathTarget(path, t("toolActivity.currentDirectory")),
          })
        : t("toolActivity.searchTarget", { target: formatSearchTarget(pattern) });
    case "web_search":
      return pattern
        ? t("toolActivity.searchWebTarget", { target: formatSearchTarget(pattern) })
        : t("toolActivity.searchWeb");
    case "web_fetch": {
      const url = stringValue(result, "url") ?? stringValue(args, "url");
      return url
        ? t("toolActivity.readWebTarget", { target: truncateText(url, 90) })
        : t("toolActivity.readWeb");
    }
    case "tool_search":
      return pattern
        ? t("toolActivity.searchToolsTarget", { target: formatSearchTarget(pattern) })
        : t("toolActivity.searchTools");
    case "load_skill": {
      const skill = stringValue(args, "name");
      return skill
        ? t("toolActivity.learnSkillTarget", { target: truncateText(skill.replace(/^\//, ""), 70) })
        : t("toolActivity.learnSkill");
    }
    case "update_plan":
      return t("toolActivity.updatePlan");
    case "bash":
      if (command.startsWith("git ")) {
        return readableCommandLabel(item);
      }
      return command
        ? t("toolActivity.runTarget", { target: truncateText(command, 100) })
        : t("toolActivity.runCommand");
    case "edit_file":
      return t("toolActivity.updateTarget", { target: formatPathTarget(path, t("toolActivity.file")) });
    case "write_file":
      return t("toolActivity.updateTarget", { target: formatPathTarget(path, t("toolActivity.file")) });
    case "apply_patch":
      return path
        ? t("toolActivity.updateTarget", { target: formatPathTarget(path, t("toolActivity.file")) })
        : t("toolActivity.updateFiles");
    case "spawn_agent": {
      const task =
        stringValue(args, "name") ??
        stringValue(args, "description") ??
        stringValue(args, "prompt");
      return task
        ? t("toolActivity.startSubtaskTarget", { target: truncateText(task, 70) })
        : t("toolActivity.startSubtask");
    }
    case "followup_task": {
      const task = stringValue(args, "target") ?? stringValue(args, "message");
      return task
        ? t("toolActivity.followupSubtaskTarget", { target: truncateText(task, 70) })
        : t("toolActivity.followupSubtask");
    }
    case "send_message": {
      const task = stringValue(args, "target") ?? stringValue(args, "message");
      return task
        ? t("toolActivity.messageSubtaskTarget", { target: truncateText(task, 70) })
        : t("toolActivity.messageSubtask");
    }
    case "wait_agent":
      return t("toolActivity.waitForSubtask");
    case "await_agents":
      return t("toolActivity.waitForSubtask");
    case "close_agent":
      return t("toolActivity.closeSubtask");
    case "list_agents":
      return t("toolActivity.viewSubtasks");
    case "agent_report":
      return t("toolActivity.readSubtaskReport");
    case "goal":
      switch (stringValue(args, "action")) {
        case "create": {
          const objective = stringValue(args, "objective");
          return objective
            ? t("toolActivity.startGoalTarget", { target: truncateText(objective, 60) })
            : t("toolActivity.startGoal");
        }
        case "update":
          return t("toolActivity.updateGoal");
        default:
          return t("toolActivity.viewGoal");
      }
    case "cron":
      switch (stringValue(args, "action")) {
        case "add": {
          const cron = stringValue(args, "cron");
          return cron
            ? t("toolActivity.scheduleTarget", { target: truncateText(cron, 60) })
            : t("toolActivity.schedule");
        }
        case "remove":
          return t("toolActivity.cancelSchedule");
        default:
          return t("toolActivity.viewSchedule");
      }
    case "browser":
      return readableBrowserLabel(args);
    default:
      return readableToolName(name);
  }
}

function richToolResultRecord(
  item: Pick<ThreadItem, "result_detail">,
): JsonRecord | undefined {
  const structured = item.result_detail?.structured_content;
  if (structured && typeof structured === "object" && !Array.isArray(structured)) {
    return structured as JsonRecord;
  }
  for (const part of item.result_detail?.content ?? []) {
    if (part.type !== "text" || !part.text) {
      continue;
    }
    const parsed = parseJSONRecord(part.text);
    if (parsed) {
      return parsed;
    }
  }
  return undefined;
}

// appendCapabilitySuffix adds the runtime-supplied capability name to
// the readable command line when the backend ships one. Format:
// "运行 npm test — command.bash". The capability is optional and the
// existing tests rely on the suffix being absent when display lacks
// it, so the change stays additive.
function appendCapabilitySuffix(text: string, capability: string | undefined): string {
  const normalized = capability?.trim();
  if (!normalized || !text) {
    return text;
  }
  return `${text} — ${normalized}`;
}

function isMCPToolName(name: string): boolean {
  return name.startsWith("mcp_");
}

function displaySectionKey(kind: string | undefined): string | undefined {
  const normalized = kind?.trim();
  switch (normalized) {
    case "read":
    case "search":
    case "command":
    case "agent":
    case "plan":
    case "interaction":
    case "schedule":
    case "browser":
    case "skill":
    case "context":
      return normalized;
    case "edit":
    case "create":
      return "change";
    case "file":
    case "web":
      return "read";
    case "discovery":
      return "search";
    case "shell":
    case "git":
    case "process":
      return "command";
    case "user_interaction":
      return "interaction";
    default:
      return undefined;
  }
}

function toolActivitySectionKey(item: ThreadItem): string {
  const capabilityKey = capabilitySectionKey(item.display?.capability);
  if (capabilityKey) {
    return capabilityKey;
  }
  const displayKey = displaySectionKey(item.display?.kind);
  if (displayKey) {
    return displayKey;
  }
  const name = (item.name ?? "").trim();
  switch (name) {
    case "read_file":
    case "list_files":
    case "web_fetch":
      return "read";
    case "grep":
    case "glob":
    case "web_search":
    case "tool_search":
      return "search";
    case "edit_file":
    case "write_file":
    case "apply_patch":
      return "change";
    case "bash":
      return "command";
    case "spawn_agent":
    case "send_message":
    case "followup_task":
    case "wait_agent":
    case "await_agents":
    case "close_agent":
    case "list_agents":
    case "agent_report":
      return "agent";
    case "update_plan":
      return "plan";
    case "cron":
      return "schedule";
    case "browser":
      return "browser";
    case "load_skill":
      return "skill";
    default:
      return "other";
  }
}

function capabilitySectionKey(capability: string | undefined): string | undefined {
  const normalized = capability?.trim();
  if (!normalized) {
    return undefined;
  }
  if (normalized.startsWith("file.edit")) {
    return "change";
  }
  if (normalized.startsWith("file.") || normalized === "web.fetch") {
    return "read";
  }
  if (normalized.startsWith("search.") || normalized === "web.search" || normalized === "tool.discovery") {
    return "search";
  }
  if (normalized.startsWith("command.")) {
    return "command";
  }
  if (normalized.startsWith("task.")) {
    return "agent";
  }
  if (normalized.startsWith("context.")) {
    return "context";
  }
  switch (normalized) {
    case "plan":
      return "plan";
    case "schedule":
      return "schedule";
    case "skill":
      return "skill";
    default:
      return undefined;
  }
}

function toolActivitySectionFromItems(
  key: string,
  items: ThreadItem[],
): ToolActivitySection {
  switch (key) {
    case "read":
      return {
        id: key,
        kind: "read",
        title: t("toolActivity.view"),
        detail: compactDetailText(compactToolTargets(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "search":
      return {
        id: key,
        kind: "search",
        title: t("toolActivity.search"),
        detail: compactDetailText(compactSearchTargets(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "change":
      return {
        id: key,
        kind: "edit",
        title: t("toolActivity.updateFiles"),
        detail: compactDetailText(compactToolTargets(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "command":
      return {
        id: key,
        kind: "command",
        title: t("toolActivity.inspect"),
        detail: compactDetailText(compactCommandLabels(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "agent":
      return {
        id: key,
        kind: "agent",
        title: t("toolActivity.subtasks"),
        detail: compactDetailText(compactAgentLabels(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "plan":
      return {
        id: key,
        kind: "plan",
        title: t("toolActivity.updatePlan"),
        detail: compactDetailText(compactPlanUpdates(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "interaction":
      return {
        id: key,
        kind: "interaction",
        title: t("toolActivity.waitingForUser"),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "schedule":
      return {
        id: key,
        kind: "schedule",
        title: readableScheduleSummary(items),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "browser":
      return {
        id: key,
        kind: "browser",
        title: t("toolActivity.browser"),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "skill":
      return {
        id: key,
        kind: "skill",
        title: t("toolActivity.learn"),
        detail: compactDetailText(compactSkillTargets(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "context":
      return {
        id: key,
        kind: "context",
        title: t("toolActivity.contextDive"),
        detail: compactDetailText(compactContextAnchors(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    default:
      return {
        id: key,
        kind: "unknown",
        title: t("toolActivity.tool"),
        detail: compactDetailText(
          uniqueStrings(items.map((item) => readableToolName(item.name))),
        ),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
  }
}

function toolActivityProcessSegmentFromItems(
  key: string,
  items: ThreadItem[],
): ToolActivityProcessSegment {
  const status = combinedToolStatus(items);
  const error = firstToolError(items);
  switch (key) {
    case "read": {
      const targets = compactToolTargets(items);
      return fileCountSegment({
        id: key,
        kind: "read",
        status,
        error,
        singularPrefix: t("toolActivity.view"),
        countPrefix: t("toolActivity.view"),
        targets,
        fallbackCount: items.length,
      });
    }
    case "search": {
      const targets = compactSearchTargets(items).map(
        compactSearchProcessTarget,
      );
      // "搜索 N 次" counts search invocations; targets are deduplicated
      // patterns and would understate repeated searches of the same pattern.
      const count = items.length;
      return count > 1
        ? {
            id: key,
            kind: "search",
            status,
            error,
            countPrefix: `${t("toolActivity.search")} `,
            count,
            countSuffix: ` ${t("toolActivity.times")}`,
          }
        : {
            id: key,
            kind: "search",
            status,
            error,
            text: targets[0]
              ? t("toolActivity.searchTarget", { target: targets[0] })
              : t("toolActivity.search"),
          };
    }
    case "change": {
      const targets = compactToolTargets(items);
      return fileCountSegment({
        id: key,
        kind: "edit",
        status,
        error,
        singularPrefix: t("toolActivity.update"),
        countPrefix: t("toolActivity.update"),
        targets,
        fallbackCount: items.length,
      });
    }
    case "command": {
      const labels = compactCommandLabels(items);
      const count = labels.length || items.length;
      return count > 1
        ? {
            id: key,
            kind: "command",
            status,
            error,
            countPrefix: `${t("toolActivity.inspect")} `,
            count,
            countSuffix: ` ${t("toolActivity.items")}`,
          }
        : {
            id: key,
            kind: "command",
            status,
            error,
            text: labels[0] ?? t("toolActivity.runCommand"),
          };
    }
    case "agent": {
      const labels = compactAgentLabels(items);
      const count = labels.length || items.length;
      return count > 1
        ? {
            id: key,
            kind: "agent",
            status,
            error,
            countPrefix: `${t("toolActivity.subtasks")} `,
            count,
            countSuffix: ` ${t("toolActivity.items")}`,
          }
        : {
            id: key,
            kind: "agent",
            status,
            error,
            text: labels[0]
              ? t("toolActivity.subtaskTarget", { target: truncateText(labels[0], 48) })
              : t("toolActivity.subtasks"),
          };
    }
    case "plan":
      return {
        id: key,
        kind: "plan",
        status,
        error,
        text: t("toolActivity.updatePlan"),
      };
    case "schedule":
      return {
        id: key,
        kind: "schedule",
        status,
        error,
        text: readableScheduleSummary(items),
      };
    case "browser":
      return {
        id: key,
        kind: "browser",
        status,
        error,
        text:
          items.length > 1
            ? t("toolActivity.inspectPage")
            : (compactDetailText(compactCommandLabels(items)) ?? t("toolActivity.inspectPage")),
      };
    case "skill": {
      const targets = compactSkillTargets(items);
      const count = targets.length || items.length;
      return count > 1
        ? {
            id: key,
            kind: "skill",
            status,
            error,
            countPrefix: `${t("toolActivity.learn")} `,
            count,
            countSuffix: ` ${t("toolActivity.items")}`,
          }
        : {
            id: key,
            kind: "skill",
            status,
            error,
            text: targets[0]
              ? t("toolActivity.learnTarget", { target: truncateText(targets[0], 48) })
              : t("toolActivity.learnSkill"),
          };
    }
    case "context":
      return {
        id: key,
        kind: "context",
        status,
        error,
        text: t("toolActivity.contextDive"),
      };
    default: {
      const names = uniqueStrings(
        items.map((item) => readableToolName(item.name)),
      );
      const count = names.length || items.length;
      return count > 1
        ? {
            id: key,
            kind: "unknown",
            status,
            error,
            countPrefix: `${t("toolActivity.tool")} `,
            count,
            countSuffix: ` ${t("toolActivity.items")}`,
          }
        : {
            id: key,
            kind: "unknown",
            status,
            error,
            text: names[0] ?? t("toolActivity.tool"),
          };
    }
  }
}

function fileCountSegment({
  id,
  kind,
  status,
  error,
  singularPrefix,
  countPrefix,
  targets,
  fallbackCount,
}: {
  id: string;
  kind: ToolActivityKind;
  status: ToolActivitySectionStatus;
  error?: string;
  singularPrefix: string;
  countPrefix: string;
  targets: string[];
  fallbackCount: number;
}): ToolActivityProcessSegment {
  const count = targets.length || fallbackCount;
  return count > 1
    ? {
        id,
        kind,
        status,
        error,
        countPrefix: `${countPrefix} `,
        count,
        countSuffix: ` ${t("toolActivity.files")}`,
      }
    : {
        id,
        kind,
        status,
        error,
        text: targets[0] ? `${singularPrefix} ${targets[0]}` : singularPrefix,
      };
}

function combinedToolStatus(items: ThreadItem[]): ToolActivitySectionStatus {
  if (items.some((item) => item.status === "failed" || item.error)) {
    return "failed";
  }
  if (items.some((item) => (item.status ?? "in_progress") === "in_progress")) {
    return "running";
  }
  return "completed";
}

function firstToolError(items: ThreadItem[]): string | undefined {
  const item = items.find((item) => item.error);
  if (!item?.error) {
    return undefined;
  }
  const display = userFacingErrorForMessage(item.error, "tool");
  return `${display.title}。${display.detail}`;
}

function compactToolTargets(items: ThreadItem[]): string[] {
  return uniqueStrings(
    items
      .flatMap((item) => {
        const args = parseJSONRecord(item.arguments);
        const result = parseJSONRecord(item.result);
        const path =
          stringValue(result, "path") ??
          stringValue(args, "path") ??
          stringValue(args, "file");
        const patchPaths = patchChangedFiles(result);
        if (patchPaths.length > 0) {
          return patchPaths.map(fileBaseName);
        }
        return path ? [fileBaseName(path)] : [];
      })
  );
}

function compactSkillTargets(items: ThreadItem[]): string[] {
  return uniqueStrings(
    items
      .map((item) => {
        const args = parseJSONRecord(item.arguments);
        const skill = stringValue(args, "name");
        return skill
          ? t("toolActivity.skillTarget", { target: truncateText(skill.replace(/^\//, ""), 70) })
          : undefined;
      })
      .filter((value): value is string => Boolean(value)),
  );
}

function compactPlanUpdates(items: ThreadItem[]): string[] {
  return uniqueStrings(
    items
      .map((item) => {
        const args = parseJSONRecord(item.arguments);
        const explanation = stringValue(args, "explanation")?.trim();
        return explanation ? truncateText(explanation, 90) : undefined;
      })
      .filter((value): value is string => Boolean(value)),
  );
}

function compactContextAnchors(items: ThreadItem[]): string[] {
  return uniqueStrings(
    items
      .map((item) => {
        const args = parseJSONRecord(item.arguments);
        const id = numberValue(args, "anchor_id");
        return id !== undefined ? `checkpoint #${id}` : undefined;
      })
      .filter((value): value is string => Boolean(value)),
  );
}

function compactSearchTargets(items: ThreadItem[]): string[] {
  return uniqueStrings(
    items
      .map((item) => {
        const args = parseJSONRecord(item.arguments);
        return (
          stringValue(args, "pattern") ??
          stringValue(args, "query") ??
          readableToolName(item.name)
        );
      })
      .filter((value): value is string => Boolean(value)),
  );
}

function compactCommandLabels(items: ThreadItem[]): string[] {
  return uniqueStrings(items.map((item) => readableCommandLabel(item)));
}

function readableScheduleSummary(items: ThreadItem[]): string {
  const actions = uniqueStrings(
    items.map((item) => {
      const args = parseJSONRecord(item.arguments);
      switch (stringValue(args, "action")) {
        case "add":
          return t("toolActivity.schedule");
        case "remove":
          return t("toolActivity.cancelSchedule");
        case "list":
          return t("toolActivity.viewSchedule");
        default:
          return t("toolActivity.handleSchedule");
      }
    }),
  );
  return actions.length === 1
    ? actions[0]
    : t("toolActivity.handleScheduleCount", { count: items.length });
}

function compactAgentLabels(items: ThreadItem[]): string[] {
  return uniqueStrings(
    items.map((item) => {
      const args = parseJSONRecord(item.arguments);
      return (
        stringValue(args, "name") ??
        stringValue(args, "description") ??
        stringValue(args, "task_name") ??
        readableToolName(item.name)
      );
    }),
  );
}

function compactDetailText(values: string[]): string | undefined {
  if (values.length === 0) {
    return undefined;
  }
  const shown = values.slice(0, 4).join(t("toolActivity.compactSeparator"));
  return values.length > 4
    ? t("toolActivity.moreItems", { shown, count: values.length })
    : shown;
}

function formatPathTarget(path: string | undefined, fallback: string): string {
  if (!path) {
    return fallback;
  }
  if (path === ".") {
    return t("toolActivity.currentDirectory");
  }
  return fileBaseName(path);
}

function formatDirectoryTarget(path: string | undefined): string {
  if (!path || path === ".") {
    return t("toolActivity.currentDirectory");
  }
  return fileBaseName(path);
}

function formatSearchTarget(pattern: string | undefined): string {
  if (!pattern) {
    return t("toolActivity.content");
  }
  return truncateText(pattern.replace(/^\*\*\//, ""), 90);
}

function compactSearchProcessTarget(pattern: string): string {
  const normalized = pattern.replace(/^\*\*\//, "").trim();
  if (!normalized) {
    return t("toolActivity.content");
  }
  const alternatives = normalized
    .split("|")
    .map((part) => part.trim())
    .filter(Boolean);
  if (alternatives.length > 1) {
    const prefix = commonPrefix(alternatives);
    const boundary = prefix.lastIndexOf("_");
    if (boundary >= 3) {
      return `${prefix.slice(0, boundary + 1)}*`;
    }
    return t("toolActivity.itemCount", { count: alternatives.length });
  }
  return truncateText(normalized, 48);
}

function commonPrefix(values: string[]): string {
  if (values.length === 0) {
    return "";
  }
  let prefix = values[0];
  for (const value of values.slice(1)) {
    while (prefix && !value.startsWith(prefix)) {
      prefix = prefix.slice(0, -1);
    }
  }
  return prefix;
}

function truncateText(text: string, max: number): string {
  return text.length > max ? `${text.slice(0, max - 1)}…` : text;
}

function readableCommandLabel(
  item: Pick<ThreadItem, "name" | "arguments" | "result">,
): string {
  const args = parseJSONRecord(item.arguments);
  const result = parseJSONRecord(item.result);
  const command =
    stringValue(result, "command") ?? stringValue(args, "command") ?? "";
  const action = stringValue(result, "action") ?? stringValue(args, "action") ?? "";
  const subcommand =
    stringValue(result, "subcommand") ?? stringValue(args, "subcommand") ?? "";
  if (
    action === "start_background" ||
    action === "read_background" ||
    action === "list_background" ||
    action === "stop_background" ||
    action === "write_background"
  ) {
    return readableBackgroundCommandLabel(action, command);
  }
  if (command.startsWith("git ")) {
    if (subcommand === "status" || command.includes("status")) {
      return t("toolActivity.checkGitStatus");
    }
    if (subcommand === "diff" || command.includes("diff")) {
      return t("toolActivity.viewDiff");
    }
    if (subcommand === "log" || command.includes("log")) {
      return t("toolActivity.viewCommitHistory");
    }
    return t("toolActivity.runGitOperation");
  }
  if (/npm\s+run\s+typecheck|tsc\s+--noEmit/.test(command)) {
    return t("toolActivity.checkTypes");
  }
  if (/npm\s+run\s+build|vite\s+build|electron-vite\s+build/.test(command)) {
    return t("toolActivity.buildApp");
  }
  if (/go\s+test|npm\s+test|pnpm\s+test|yarn\s+test/.test(command)) {
    return t("toolActivity.runTests");
  }
  if (/(?:^|[;&|]\s*)date(?:\s|$)/.test(command)) {
    return t("toolActivity.viewLocalTime");
  }
  if (/(?:^|[;&|]\s*)sqlite3\b|(?:import|from)\s+sqlite3\b/.test(command)) {
    return t("toolActivity.useLocalDatabase");
  }
  return t("toolActivity.runCommand");
}

function readableBackgroundCommandLabel(action: string, command: string): string {
  switch (action) {
    case "start_background":
      return command
        ? t("toolActivity.startTarget", { target: truncateText(command, 100) })
        : t("toolActivity.startBackgroundTask");
    case "read_background":
      return t("toolActivity.readBackgroundOutput");
    case "list_background":
      return t("toolActivity.viewBackgroundTasks");
    case "stop_background":
      return t("toolActivity.stopBackgroundTask");
    case "write_background":
      return t("toolActivity.writeBackgroundInput");
    default:
      return command
        ? t("toolActivity.startTarget", { target: truncateText(command, 100) })
        : t("toolActivity.backgroundTask");
  }
}

function readableBrowserLabel(args: JsonRecord | undefined): string {
  const action = (stringValue(args, "action") ?? "").toLowerCase();
  if (action === "navigate" || action === "open") {
    const url = stringValue(args, "url");
    return url
      ? t("toolActivity.openBrowserTarget", { target: truncateText(url, 90) })
      : t("toolActivity.openBrowser");
  }
  if (action === "click") {
    return t("toolActivity.clickBrowser");
  }
  if (action === "type") {
    return t("toolActivity.typeInBrowser");
  }
  if (action === "screenshot") {
    return t("toolActivity.captureBrowser");
  }
  if (action === "evaluate") {
    return t("toolActivity.runBrowserScript");
  }
  return t("toolActivity.useBrowser");
}

export function readableToolName(name: string | undefined): string {
  switch ((name ?? "").trim()) {
    case "read_file":
      return t("toolActivity.viewFile");
    case "list_files":
      return t("toolActivity.viewDirectory");
    case "grep":
      return t("toolActivity.searchContent");
    case "glob":
      return t("toolActivity.matchFiles");
    case "edit_file":
      return t("toolActivity.editFile");
    case "write_file":
      return t("toolActivity.writeFile");
    case "apply_patch":
      return t("toolActivity.updateFiles");
    case "web_search":
      return t("toolActivity.searchWeb");
    case "web_fetch":
      return t("toolActivity.readWeb");
    case "bash":
      return t("toolActivity.runCommand");
    case "tool_search":
      return t("toolActivity.searchTools");
    case "load_skill":
      return t("toolActivity.learnSkill");
    case "update_plan":
      return t("toolActivity.updatePlan");
    case "cron":
      return t("toolActivity.scheduledTask");
    case "browser":
      return t("toolActivity.browser");
    default:
      return name?.trim() || t("toolActivity.tool");
  }
}

function uniqueStrings(values: string[]): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const value of values) {
    const normalized = value.trim();
    if (!normalized || seen.has(normalized)) {
      continue;
    }
    seen.add(normalized);
    out.push(normalized);
  }
  return out;
}

export function summarizeToolActivity(items: ThreadItem[]): ToolActivitySummary {
  const readFiles = new Set<string>();
  const searchedFiles = new Set<string>();
  const editedFiles = new Set<string>();
  const createdFiles = new Set<string>();
  const unknownTools = new Set<string>();
  let searchCount = 0;
  let listCount = 0;
  let commandCount = 0;
  let agentCount = 0;
  let additions = 0;
  let deletions = 0;
  let running = false;
  let failed = false;
  let primaryKind: ToolActivityKind = "unknown";

  for (const item of items) {
    const name = (item.name ?? "tool").trim() || "tool";
    const args = parseJSONRecord(item.arguments);
    const result = parseJSONRecord(item.result);
    const capability = item.display?.capability?.trim();
    const path =
      stringValue(result, "path") ??
      stringValue(args, "path") ??
      stringValue(args, "file");

    running = running || (item.status ?? "in_progress") === "in_progress";
    failed = failed || item.status === "failed" || Boolean(item.error);

    if (name === "read_file") {
      primaryKind = primaryKind === "unknown" ? "read" : primaryKind;
      addPath(readFiles, path);
      continue;
    }
    if (name === "grep" || name === "glob" || name === "web_search") {
      primaryKind =
        primaryKind === "unknown" || primaryKind === "read"
          ? "search"
          : primaryKind;
      searchCount++;
      collectResultFiles(result, searchedFiles);
      continue;
    }
    if (name === "list_files") {
      primaryKind = primaryKind === "unknown" ? "list" : primaryKind;
      listCount++;
      continue;
    }
    if (name === "bash" || capability?.startsWith("command.")) {
      primaryKind = primaryKind === "unknown" ? "command" : primaryKind;
      commandCount++;
      continue;
    }
    if (name === "edit_file" || name === "write_file" || name === "apply_patch" || capability === "file.edit") {
      const diff = summarizeDiff(result);
      const target = diff.newFile ? createdFiles : editedFiles;
      const patchPaths = patchChangedFiles(result);
      if (patchPaths.length > 0) {
        for (const patchPath of patchPaths) {
          addPath(target, patchPath);
        }
      } else {
        addPath(target, path);
      }
      additions += diff.additions;
      deletions += diff.deletions;
      primaryKind = diff.newFile ? "create" : "edit";
      continue;
    }
    if (
      name === "spawn_agent" ||
      name === "fork_agent" ||
      name === "send_message"
    ) {
      primaryKind = primaryKind === "unknown" ? "agent" : primaryKind;
      agentCount++;
      continue;
    }
    unknownTools.add(name);
  }

  const singleChangedFile =
    editedFiles.size + createdFiles.size === 1 && items.length === 1;
  if (singleChangedFile) {
    const created = createdFiles.size === 1;
    const filePath = firstSetValue(created ? createdFiles : editedFiles);
    return {
      kind: created ? "create" : "edit",
      text: failed
        ? t("toolActivity.editFailed")
        : created
          ? running
            ? t("toolActivity.creating")
            : t("toolActivity.created")
          : running
            ? t("toolActivity.editing")
            : t("toolActivity.edited"),
      fileName: filePath ? fileBaseName(filePath) : undefined,
      additions,
      deletions,
      running,
      failed,
    };
  }

  const parts: string[] = [];
  if (createdFiles.size > 0) {
    parts.push(t(createdFiles.size === 1 ? "toolActivity.createdFile" : "toolActivity.createdFiles", { count: createdFiles.size }));
  }
  if (editedFiles.size > 0) {
    parts.push(t(editedFiles.size === 1 ? "toolActivity.editedFile" : "toolActivity.editedFiles", { count: editedFiles.size }));
  }
  if (readFiles.size > 0) {
    parts.push(t(readFiles.size === 1 ? "toolActivity.exploredFile" : "toolActivity.exploredFiles", { count: readFiles.size }));
  }
  if (searchedFiles.size > 0) {
    parts.push(t(searchedFiles.size === 1 ? "toolActivity.searchedFile" : "toolActivity.searchedFiles", { count: searchedFiles.size }));
  }
  if (searchCount > 0) {
    parts.push(t(searchCount === 1 ? "toolActivity.searchCountOne" : "toolActivity.searchCount", { count: searchCount }));
  }
  if (listCount > 0) {
    parts.push(t(listCount === 1 ? "toolActivity.listCountOne" : "toolActivity.listCount", { count: listCount }));
  }
  if (commandCount > 0) {
    parts.push(t(commandCount === 1 ? "toolActivity.commandCountOne" : "toolActivity.commandCount", { count: commandCount }));
  }
  if (agentCount > 0) {
    parts.push(t(agentCount === 1 ? "toolActivity.startedSubtaskCountOne" : "toolActivity.startedSubtaskCount", { count: agentCount }));
  }
  if (parts.length === 0 && unknownTools.size > 0) {
    const names = Array.from(unknownTools).slice(0, 2).join(t("toolActivity.compactSeparator"));
    parts.push(t(running ? "toolActivity.callingTools" : "toolActivity.calledTools", { names }));
  }
  if (parts.length === 0) {
    parts.push(t(running ? "toolActivity.usingTools" : "toolActivity.usedTools"));
  }

  return {
    kind: primaryKind,
    text: failed
      ? t("toolActivity.failedSummary", { summary: parts.join(" · ") })
      : parts.join(" · "),
    additions,
    deletions,
    running,
    failed,
  };
}

/**
 * One external page the agent consulted in a turn via web_search or
 * web_fetch. The renderer uses this list to draw the favicon pill at the
 * bottom of an assistant message (mirroring the ChatGPT / Claude
 * "来源" treatment).
 *
 * `host` is the canonical domain used both for favicon lookup and for
 * dedupe: multiple pages on the same domain collapse to a single icon.
 */
export type TurnSource = {
  url: string;
  host: string;
  title?: string;
  origin: "web_search" | "web_fetch";
};

/**
 * Collect the unique external sources a turn consulted through
 * `web_search` and `web_fetch`. Order is first-seen across the turn.
 *
 * web_search contributes one source per hit (each result has its own
 * page). web_fetch contributes one source per call (the URL the agent
 * asked to read). Both are deduped by host so multiple pages from the
 * same domain collapse to a single icon — that matches what users
 * expect from the ChatGPT / Claude sources row and keeps the pill
 * readable when the agent makes many hits on docs.anthropic.com.
 */
export function collectTurnSources(items: ThreadItem[]): TurnSource[] {
  const byHost = new Map<string, TurnSource>();
  for (const item of items) {
    if (item.type !== "tool_call" && item.type !== "collab_agent_tool_call") {
      continue;
    }
    const name = (item.name ?? "").trim();
    if (name === "web_search") {
      const result = parseJSONRecord(item.result);
      for (const hit of arrayValue(result, "results")) {
        if (!isRecord(hit)) continue;
        const url = stringValue(hit, "url");
        if (!url) continue;
        addSource(byHost, { url, title: stringValue(hit, "title"), origin: "web_search" });
      }
      continue;
    }
    if (name === "web_fetch") {
      const args = parseJSONRecord(item.arguments);
      const result = parseJSONRecord(item.result);
      const url = stringValue(args, "url") ?? stringValue(result, "url");
      if (!url) continue;
      addSource(byHost, { url, origin: "web_fetch" });
      continue;
    }
  }
  return Array.from(byHost.values());
}

function addSource(
  byHost: Map<string, TurnSource>,
  candidate: Omit<TurnSource, "host">,
): void {
  const host = normalizeHost(candidate.url);
  if (!host) return;
  const existing = byHost.get(host);
  if (!existing) {
    byHost.set(host, { ...candidate, host });
    return;
  }
  // First-seen wins for the canonical URL, but upgrade title when the
  // existing slot doesn't have one and the new candidate does — common
  // when the first hit was a fetch with no title and a later search hit
  // surfaces a titled result for the same domain.
  if (!existing.title && candidate.title) {
    byHost.set(host, { ...existing, title: candidate.title });
  }
}

function normalizeHost(rawUrl: string): string | undefined {
  let parsed: URL;
  try {
    parsed = new URL(rawUrl);
  } catch {
    return undefined;
  }
  const host = parsed.hostname.toLowerCase();
  if (!host) return undefined;
  return host.startsWith("www.") ? host.slice(4) : host;
}

function summarizeDiff(result: JsonRecord | undefined): DiffStats {
  const riskSummary = recordValue(result, "risk_summary");
  if (riskSummary) {
    return {
      additions: numberValue(riskSummary, "added_lines") ?? 0,
      deletions: numberValue(riskSummary, "deleted_lines") ?? 0,
      newFile: false,
    };
  }
  const diff = recordValue(result, "diff");
  if (!diff) {
    return { additions: 0, deletions: 0, newFile: false };
  }
  const newFile = diff.new_file === true;
  if (newFile) {
    return {
      additions: numberValue(diff, "lines") ?? 0,
      deletions: 0,
      newFile,
    };
  }

  let additions = 0;
  let deletions = 0;
  for (const hunk of arrayValue(diff, "hunks")) {
    if (!isRecord(hunk)) {
      continue;
    }
    for (const line of arrayValue(hunk, "lines")) {
      if (!isRecord(line)) {
        continue;
      }
      if (line.op === "insert") {
        additions++;
      } else if (line.op === "delete") {
        deletions++;
      }
    }
  }
  return { additions, deletions, newFile };
}

function patchChangedFiles(result: JsonRecord | undefined): string[] {
  const changedFiles = arrayValue(result, "changed_files").filter(
    (file): file is string => typeof file === "string" && file.trim().length > 0,
  );
  if (changedFiles.length > 0) {
    return changedFiles.map((file) => file.trim());
  }
  return arrayValue(result, "files")
    .flatMap((file) => {
      if (!isRecord(file)) {
        return [];
      }
      const path = stringValue(file, "path");
      const movePath = stringValue(file, "move_path");
      return [path, movePath].filter((value): value is string => Boolean(value));
    });
}

function collectResultFiles(
  result: JsonRecord | undefined,
  output: Set<string>,
): void {
  for (const file of arrayValue(result, "files")) {
    if (typeof file === "string" && file.trim()) {
      output.add(file.trim());
    }
  }
  for (const match of arrayValue(result, "matches")) {
    if (isRecord(match)) {
      addPath(output, stringValue(match, "file"));
    }
  }
  for (const count of arrayValue(result, "counts")) {
    if (isRecord(count)) {
      addPath(output, stringValue(count, "file"));
    }
  }
}

function parseJSONRecord(value: string | undefined): JsonRecord | undefined {
  if (!value?.trim()) {
    return undefined;
  }
  try {
    const parsed: unknown = JSON.parse(value);
    return isRecord(parsed) ? parsed : undefined;
  } catch {
    return undefined;
  }
}

export function isRecord(value: unknown): value is JsonRecord {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

export function recordValue(
  record: JsonRecord | undefined,
  key: string,
): JsonRecord | undefined {
  if (!record) {
    return undefined;
  }
  const value = record[key];
  return isRecord(value) ? value : undefined;
}

function arrayValue(record: JsonRecord | undefined, key: string): unknown[] {
  if (!record) {
    return [];
  }
  const value = record[key];
  return Array.isArray(value) ? value : [];
}

export function stringValue(
  record: JsonRecord | undefined,
  key: string,
): string | undefined {
  if (!record) {
    return undefined;
  }
  const value = record[key];
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

export function numberValue(
  record: JsonRecord | undefined,
  key: string,
): number | undefined {
  if (!record) {
    return undefined;
  }
  const value = record[key];
  return typeof value === "number" && Number.isFinite(value)
    ? value
    : undefined;
}

function addPath(output: Set<string>, path: string | undefined): void {
  if (path?.trim()) {
    output.add(path.trim());
  }
}

function firstSetValue(values: Set<string>): string | undefined {
  for (const value of values) {
    return value;
  }
  return undefined;
}

function fileBaseName(path: string): string {
  const normalized = path.replace(/\\/g, "/");
  const parts = normalized.split("/").filter(Boolean);
  return parts[parts.length - 1] ?? path;
}
