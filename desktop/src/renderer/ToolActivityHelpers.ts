import { formatMessageFlowCommand } from "./message-flow-display";
import type { ThreadItem } from "../shared/protocol";
import { userFacingErrorForMessage } from "./UserFacingErrors";

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
  return fallback.failed
    ? `未完成 · ${fragments.join("，")}`
    : fragments.join("，");
}

function sectionSummaryText(section: ToolActivitySection): string {
  return section.title;
}

function toolCommands(items: ThreadItem[]): ToolActivityCommand[] {
  return items.map((item) => ({
    text: readableToolActivityCommand(item),
    status: itemToolStatus(item),
  }));
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
  item: Pick<ThreadItem, "name" | "arguments" | "result" | "display">,
): string {
  const displayText = item.display?.text?.trim();
  if (displayText) {
    return displayText;
  }

  const args = parseJSONRecord(item.arguments);
  const result = parseJSONRecord(item.result);
  const name = (item.name ?? "").trim();
  if (isMCPToolName(name)) {
    return rawToolCommand(name, item.arguments);
  }
  const path =
    stringValue(result, "path") ??
    stringValue(args, "path") ??
    stringValue(args, "file");
  const command =
    stringValue(result, "command") ?? stringValue(args, "command") ?? "";
  const pattern =
    stringValue(args, "pattern") ??
    stringValue(args, "query") ??
    stringValue(args, "q");

  switch (name) {
    case "read_file":
      return `读取 ${formatPathTarget(path, "文件")}`;
    case "list_files":
      return path && path !== "."
        ? `查看 ${formatDirectoryTarget(path)}`
        : "查看项目目录";
    case "grep":
    case "glob":
      return `搜索 ${formatSearchTarget(pattern)}`;
    case "web_search":
      return pattern ? `搜索网页 ${formatSearchTarget(pattern)}` : "搜索网页";
    case "web_fetch": {
      const url = stringValue(args, "url") ?? stringValue(result, "url");
      return url ? `读取网页 ${truncateText(url, 90)}` : "读取网页";
    }
    case "tool_search":
      return pattern ? `搜索工具 ${formatSearchTarget(pattern)}` : "搜索工具";
    case "load_skill": {
      const skill = stringValue(args, "name");
      return skill
        ? `学习 ${truncateText(skill.replace(/^\//, ""), 70)} 技能`
        : "学习技能";
    }
    case "update_plan":
      return "更新计划";
    case "git":
      return readableCommandLabel(item);
    case "run_shell":
      return command ? `运行 ${truncateText(command, 100)}` : "运行命令";
    case "start_process":
      return command ? `启动 ${truncateText(command, 100)}` : "启动后台任务";
    case "list_processes":
      return "查看后台任务";
    case "read_process_output":
      return "读取后台输出";
    case "stop_process":
      return "停止后台任务";
    case "write_stdin":
      return "写入后台输入";
    case "edit_file":
      return `编辑 ${formatPathTarget(path, "文件")}`;
    case "write_file":
      return `写入 ${formatPathTarget(path, "文件")}`;
    case "spawn_agent": {
      const task =
        stringValue(args, "name") ??
        stringValue(args, "description") ??
        stringValue(args, "prompt");
      return task ? `启动子任务 ${truncateText(task, 70)}` : "启动子任务";
    }
    case "followup_task": {
      const task = stringValue(args, "target") ?? stringValue(args, "message");
      return task ? `追加子任务 ${truncateText(task, 70)}` : "追加子任务";
    }
    case "send_message": {
      const task = stringValue(args, "target") ?? stringValue(args, "message");
      return task ? `发送给子任务 ${truncateText(task, 70)}` : "发送给子任务";
    }
    case "wait_agent":
      return "等待子任务";
    case "await_agents":
      return "等待子任务";
    case "close_agent":
      return "关闭子任务";
    case "list_agents":
      return "查看子任务";
    case "agent_report":
      return "读取子任务报告";
    case "schedule_cron": {
      const cron = stringValue(args, "cron");
      return cron ? `安排定时任务 ${truncateText(cron, 60)}` : "安排定时任务";
    }
    case "cancel_cron":
      return "取消定时任务";
    case "list_cron":
      return "查看定时任务";
    case "browser":
      return readableBrowserLabel(args);
    default:
      return readableToolName(name);
  }
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
      return "change";
    case "run_shell":
    case "git":
    case "start_process":
    case "list_processes":
    case "read_process_output":
    case "stop_process":
    case "write_stdin":
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
    case "schedule_cron":
    case "cancel_cron":
    case "list_cron":
      return "schedule";
    case "browser":
      return "browser";
    case "load_skill":
      return "skill";
    default:
      return "other";
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
        title: "查看",
        detail: compactDetailText(compactToolTargets(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "search":
      return {
        id: key,
        kind: "search",
        title: "搜索",
        detail: compactDetailText(compactSearchTargets(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "change":
      return {
        id: key,
        kind: "edit",
        title: "更新文件",
        detail: compactDetailText(compactToolTargets(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "command":
      return {
        id: key,
        kind: "command",
        title: "检查",
        detail: compactDetailText(compactCommandLabels(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "agent":
      return {
        id: key,
        kind: "agent",
        title: "子任务",
        detail: compactDetailText(compactAgentLabels(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "plan":
      return {
        id: key,
        kind: "plan",
        title: "更新计划",
        detail: compactDetailText(compactPlanUpdates(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "interaction":
      return {
        id: key,
        kind: "interaction",
        title: "等待用户",
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "schedule":
      return {
        id: key,
        kind: "schedule",
        title: "定时任务",
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "browser":
      return {
        id: key,
        kind: "browser",
        title: "浏览器",
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    case "skill":
      return {
        id: key,
        kind: "skill",
        title: "学习",
        detail: compactDetailText(compactSkillTargets(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
    default:
      return {
        id: key,
        kind: "unknown",
        title: "工具",
        detail: compactDetailText(
          uniqueStrings(items.map((item) => readableToolName(item.name))),
        ),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items),
      };
  }
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
      .map((item) => {
        const args = parseJSONRecord(item.arguments);
        const result = parseJSONRecord(item.result);
        const path =
          stringValue(result, "path") ??
          stringValue(args, "path") ??
          stringValue(args, "file");
        return path ? fileBaseName(path) : undefined;
      })
      .filter((value): value is string => Boolean(value)),
  );
}

function compactSkillTargets(items: ThreadItem[]): string[] {
  return uniqueStrings(
    items
      .map((item) => {
        const args = parseJSONRecord(item.arguments);
        const skill = stringValue(args, "name");
        return skill
          ? `${truncateText(skill.replace(/^\//, ""), 70)} 技能`
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
  const shown = values.slice(0, 4).join("、");
  return values.length > 4 ? `${shown} 等 ${values.length} 项` : shown;
}

function formatPathTarget(path: string | undefined, fallback: string): string {
  if (!path) {
    return fallback;
  }
  if (path === ".") {
    return "当前目录";
  }
  return fileBaseName(path);
}

function formatDirectoryTarget(path: string | undefined): string {
  if (!path || path === ".") {
    return "当前目录";
  }
  return fileBaseName(path);
}

function formatSearchTarget(pattern: string | undefined): string {
  if (!pattern) {
    return "内容";
  }
  return truncateText(pattern.replace(/^\*\*\//, ""), 90);
}

function truncateText(text: string, max: number): string {
  return text.length > max ? `${text.slice(0, max - 1)}…` : text;
}

function readableCommandLabel(
  item: Pick<ThreadItem, "name" | "arguments" | "result">,
): string {
  const args = parseJSONRecord(item.arguments);
  const result = parseJSONRecord(item.result);
  const name = (item.name ?? "").trim();
  const command =
    stringValue(result, "command") ?? stringValue(args, "command") ?? "";
  const subcommand =
    stringValue(result, "subcommand") ?? stringValue(args, "subcommand") ?? "";
  if (name === "git" || command.startsWith("git ")) {
    if (subcommand === "status" || command.includes("status")) {
      return "检查 Git 状态";
    }
    if (subcommand === "diff" || command.includes("diff")) {
      return "查看代码差异";
    }
    if (subcommand === "log" || command.includes("log")) {
      return "查看提交历史";
    }
    return "执行 Git 操作";
  }
  if (/npm\s+run\s+typecheck|tsc\s+--noEmit/.test(command)) {
    return "检查类型";
  }
  if (/npm\s+run\s+build|vite\s+build|electron-vite\s+build/.test(command)) {
    return "构建应用";
  }
  if (/go\s+test|npm\s+test|pnpm\s+test|yarn\s+test/.test(command)) {
    return "运行测试";
  }
  if (name === "read_process_output") {
    return "读取后台输出";
  }
  if (name === "start_process") {
    return "启动后台任务";
  }
  if (name === "stop_process") {
    return "停止后台任务";
  }
  return "运行命令";
}

function readableBrowserLabel(args: JsonRecord | undefined): string {
  const action = (stringValue(args, "action") ?? "").toLowerCase();
  if (action === "navigate" || action === "open") {
    const url = stringValue(args, "url");
    return url ? `打开浏览器 ${truncateText(url, 90)}` : "打开浏览器";
  }
  if (action === "click") {
    return "点击浏览器";
  }
  if (action === "type") {
    return "输入浏览器文本";
  }
  if (action === "screenshot") {
    return "截取浏览器";
  }
  if (action === "evaluate") {
    return "执行浏览器脚本";
  }
  return "操作浏览器";
}

export function readableToolName(name: string | undefined): string {
  switch ((name ?? "").trim()) {
    case "read_file":
      return "查看文件";
    case "list_files":
      return "查看目录";
    case "grep":
      return "搜索内容";
    case "glob":
      return "匹配文件";
    case "edit_file":
      return "编辑文件";
    case "write_file":
      return "写入文件";
    case "web_search":
      return "搜索网页";
    case "web_fetch":
      return "读取网页";
    case "run_shell":
      return "运行命令";
    case "git":
      return "Git 操作";
    case "tool_search":
      return "搜索工具";
    case "load_skill":
      return "学习技能";
    case "update_plan":
      return "更新计划";
    case "start_process":
    case "list_processes":
    case "read_process_output":
    case "stop_process":
    case "write_stdin":
      return "后台任务";
    case "schedule_cron":
    case "cancel_cron":
    case "list_cron":
      return "定时任务";
    case "browser":
      return "浏览器";
    default:
      return name?.trim() || "工具";
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
    if (name === "run_shell" || name === "git") {
      primaryKind = primaryKind === "unknown" ? "command" : primaryKind;
      commandCount++;
      continue;
    }
    if (name === "edit_file" || name === "write_file") {
      const diff = summarizeDiff(result);
      const target = diff.newFile ? createdFiles : editedFiles;
      addPath(target, path);
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
        ? "编辑失败"
        : created
          ? running
            ? "正在创建"
            : "已创建"
          : running
            ? "正在编辑"
            : "已编辑",
      fileName: filePath ? fileBaseName(filePath) : undefined,
      additions,
      deletions,
      running,
      failed,
    };
  }

  const parts: string[] = [];
  if (createdFiles.size > 0) {
    parts.push(`已创建 ${createdFiles.size} 个文件`);
  }
  if (editedFiles.size > 0) {
    parts.push(`已编辑 ${editedFiles.size} 个文件`);
  }
  if (readFiles.size > 0) {
    parts.push(`已探索 ${readFiles.size} 个文件`);
  }
  if (searchedFiles.size > 0) {
    parts.push(`已搜索 ${searchedFiles.size} 个文件`);
  }
  if (searchCount > 0) {
    parts.push(`${searchCount} 次搜索`);
  }
  if (listCount > 0) {
    parts.push(`${listCount} 次列表`);
  }
  if (commandCount > 0) {
    parts.push(`已运行 ${commandCount} 条命令`);
  }
  if (agentCount > 0) {
    parts.push(`已启动 ${agentCount} 个子任务`);
  }
  if (parts.length === 0 && unknownTools.size > 0) {
    const names = Array.from(unknownTools).slice(0, 2).join("、");
    parts.push(`${running ? "正在调用" : "已调用"} ${names}`);
  }
  if (parts.length === 0) {
    parts.push(running ? "正在使用工具" : "已使用工具");
  }

  return {
    kind: primaryKind,
    text: `${failed ? "工具失败 · " : ""}${parts.join(" · ")}`,
    additions,
    deletions,
    running,
    failed,
  };
}

function summarizeDiff(result: JsonRecord | undefined): DiffStats {
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
