import { ChevronDown } from "lucide-react";
import { useEffect, useState } from "react";
import type { ThreadItem } from "../shared/protocol";
import { userFacingErrorForMessage } from "./UserFacingErrors";

type ToolActivityKind = "edit" | "create" | "search" | "read" | "list" | "command" | "agent" | "unknown";

type ToolActivitySummary = {
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

export function ToolActivityRow({ items, collapseWhenIdle = false }: { items: ThreadItem[]; collapseWhenIdle?: boolean }): JSX.Element {
  const summary = summarizeToolActivity(items);
  const sections = buildToolActivitySections(items);
  const summaryText = activitySummaryText(sections, summary);
  const shouldExpandForStatus = summary.running || summary.failed;
  const [expanded, setExpanded] = useState(!collapseWhenIdle && shouldExpandForStatus);
  const className = `activity-group${expanded ? " expanded" : ""}${summary.running ? " running" : ""}${
    summary.failed ? " failed" : ""
  }`;

  useEffect(() => {
    if (shouldExpandForStatus) {
      setExpanded(true);
      return;
    }
    if (collapseWhenIdle) {
      setExpanded(false);
    }
  }, [collapseWhenIdle, shouldExpandForStatus]);

  return (
    <article className={className}>
      <button
        className="activity-row activity-toggle"
        type="button"
        aria-expanded={expanded}
        onClick={() => setExpanded((open) => !open)}
      >
        <span className="activity-copy">
          <span>{summaryText}</span>
          {summary.fileName ? <span className="activity-file">{summary.fileName}</span> : null}
          {summary.additions > 0 ? <span className="activity-add">+{summary.additions}</span> : null}
          {summary.deletions > 0 ? <span className="activity-delete">-{summary.deletions}</span> : null}
        </span>
        <ChevronDown className="activity-chevron" size={13} />
      </button>
      <div className="activity-details" aria-hidden={!expanded}>
        <div className="activity-details-inner">
          {sections.map((section) => (
            <ToolActivitySectionView key={section.id} section={section} />
          ))}
        </div>
      </div>
    </article>
  );
}

type ToolActivitySectionStatus = "running" | "completed" | "failed";

type ToolActivitySection = {
  id: string;
  kind: ToolActivityKind;
  title: string;
  subtitle?: string;
  detail?: string;
  status: ToolActivitySectionStatus;
  commands: string[];
  error?: string;
};

function ToolActivitySectionView({ section }: { section: ToolActivitySection }): JSX.Element {
  return (
    <section className="activity-detail">
      <div className="activity-detail-body">
        <div className="activity-command-list">
          {section.commands.map((command, index) => (
            <code className="activity-command" key={`${section.id}-${index}`}>
              {command}
            </code>
          ))}
        </div>
        {section.error ? <div className="activity-detail-error">{section.error}</div> : null}
      </div>
    </section>
  );
}

function buildToolActivitySections(items: ThreadItem[]): ToolActivitySection[] {
  const groups = new Map<string, ThreadItem[]>();
  for (const item of items) {
    const key = toolActivitySectionKey(item);
    groups.set(key, [...(groups.get(key) ?? []), item]);
  }
  return Array.from(groups.entries()).map(([key, grouped]) => toolActivitySectionFromItems(key, grouped));
}

function activitySummaryText(sections: ToolActivitySection[], fallback: ToolActivitySummary): string {
  if (sections.length === 0) {
    return fallback.text;
  }
  const fragments = sections.map((section) => sectionSummaryText(section)).filter(Boolean);
  if (fragments.length === 0) {
    return fallback.text;
  }
  return fallback.failed ? `未完成 · ${fragments.join("，")}` : fragments.join("，");
}

function sectionSummaryText(section: ToolActivitySection): string {
  return section.title;
}

function toolCommands(items: ThreadItem[]): string[] {
  return items.map((item) => {
    const name = item.name?.trim() || "tool";
    const args = item.arguments?.trim();
    return args ? `${name} ${args}` : name;
  });
}

function toolActivitySectionKey(item: ThreadItem): string {
  const name = (item.name ?? "").trim();
  switch (name) {
    case "read_file":
    case "list_files":
      return "read";
    case "grep":
    case "glob":
    case "web_search":
      return "search";
    case "edit_file":
    case "write_file":
      return "change";
    case "run_shell":
    case "git":
    case "start_process":
    case "read_process_output":
    case "stop_process":
      return "command";
    case "spawn_agent":
    case "send_message":
    case "followup_task":
    case "wait_agent":
    case "close_agent":
    case "list_agents":
      return "agent";
    default:
      return "other";
  }
}

function toolActivitySectionFromItems(key: string, items: ThreadItem[]): ToolActivitySection {
  switch (key) {
    case "read":
      return {
        id: key,
        kind: "read",
        title: `查看 ${items.length} 处`,
        detail: compactDetailText(compactToolTargets(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items)
      };
    case "search":
      return {
        id: key,
        kind: "search",
        title: `搜索 ${items.length} 次`,
        detail: compactDetailText(compactSearchTargets(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items)
      };
    case "change":
      return {
        id: key,
        kind: "edit",
        title: `更新 ${items.length} 个文件`,
        detail: compactDetailText(compactToolTargets(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items)
      };
    case "command":
      return {
        id: key,
        kind: "command",
        title: `检查 ${items.length} 项`,
        detail: compactDetailText(compactCommandLabels(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items)
      };
    case "agent":
      return {
        id: key,
        kind: "agent",
        title: `子任务 ${items.length} 项`,
        detail: compactDetailText(compactAgentLabels(items)),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items)
      };
    default:
      return {
        id: key,
        kind: "unknown",
        title: `工具 ${items.length} 项`,
        detail: compactDetailText(uniqueStrings(items.map((item) => readableToolName(item.name)))),
        status: combinedToolStatus(items),
        commands: toolCommands(items),
        error: firstToolError(items)
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
        const path = stringValue(result, "path") ?? stringValue(args, "path") ?? stringValue(args, "file");
        return path ? fileBaseName(path) : undefined;
      })
      .filter((value): value is string => Boolean(value))
  );
}

function compactSearchTargets(items: ThreadItem[]): string[] {
  return uniqueStrings(
    items
      .map((item) => {
        const args = parseJSONRecord(item.arguments);
        return stringValue(args, "pattern") ?? stringValue(args, "query") ?? readableToolName(item.name);
      })
      .filter((value): value is string => Boolean(value))
  );
}

function compactCommandLabels(items: ThreadItem[]): string[] {
  return uniqueStrings(items.map((item) => readableCommandLabel(item)));
}

function compactAgentLabels(items: ThreadItem[]): string[] {
  return uniqueStrings(
    items.map((item) => {
      const args = parseJSONRecord(item.arguments);
      return stringValue(args, "task_name") ?? readableToolName(item.name);
    })
  );
}

function compactDetailText(values: string[]): string | undefined {
  if (values.length === 0) {
    return undefined;
  }
  const shown = values.slice(0, 4).join("、");
  return values.length > 4 ? `${shown} 等 ${values.length} 项` : shown;
}

function readableCommandLabel(item: ThreadItem): string {
  const args = parseJSONRecord(item.arguments);
  const result = parseJSONRecord(item.result);
  const name = (item.name ?? "").trim();
  const command = stringValue(result, "command") ?? stringValue(args, "command") ?? "";
  const subcommand = stringValue(result, "subcommand") ?? stringValue(args, "subcommand") ?? "";
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

function ActivityIcon({ kind, failed, size = 14 }: { kind: ToolActivityKind; failed: boolean; size?: number }): JSX.Element {
  if (failed) {
    return <AlertCircle size={size} />;
  }
  switch (kind) {
    case "edit":
    case "create":
      return <Pencil size={size} />;
    case "search":
      return <Search size={size} />;
    case "read":
      return <FileText size={size} />;
    case "list":
      return <ListIcon size={size} />;
    case "command":
      return <Terminal size={size} />;
    case "agent":
      return <MessageSquarePlus size={size} />;
    default:
      return <Wrench size={size} />;
  }
}

function summarizeToolActivity(items: ThreadItem[]): ToolActivitySummary {
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
    const path = stringValue(result, "path") ?? stringValue(args, "path") ?? stringValue(args, "file");

    running = running || (item.status ?? "in_progress") === "in_progress";
    failed = failed || item.status === "failed" || Boolean(item.error);

    if (name === "read_file") {
      primaryKind = primaryKind === "unknown" ? "read" : primaryKind;
      addPath(readFiles, path);
      continue;
    }
    if (name === "grep" || name === "glob" || name === "web_search") {
      primaryKind = primaryKind === "unknown" || primaryKind === "read" ? "search" : primaryKind;
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
    if (name === "spawn_agent" || name === "fork_agent" || name === "send_message") {
      primaryKind = primaryKind === "unknown" ? "agent" : primaryKind;
      agentCount++;
      continue;
    }
    unknownTools.add(name);
  }

  const singleChangedFile = editedFiles.size + createdFiles.size === 1 && items.length === 1;
  if (singleChangedFile) {
    const created = createdFiles.size === 1;
    const filePath = firstSetValue(created ? createdFiles : editedFiles);
    return {
      kind: created ? "create" : "edit",
      text: failed ? "编辑失败" : created ? (running ? "正在创建" : "已创建") : running ? "正在编辑" : "已编辑",
      fileName: filePath ? fileBaseName(filePath) : undefined,
      additions,
      deletions,
      running,
      failed
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
    failed
  };
}

function summarizeDiff(result: JsonRecord | undefined): DiffStats {
  const diff = recordValue(result, "diff");
  if (!diff) {
    return { additions: 0, deletions: 0, newFile: false };
  }
  const newFile = diff.new_file === true;
  if (newFile) {
    return { additions: numberValue(diff, "lines") ?? 0, deletions: 0, newFile };
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

function collectResultFiles(result: JsonRecord | undefined, output: Set<string>): void {
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

export function recordValue(record: JsonRecord | undefined, key: string): JsonRecord | undefined {
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

export function stringValue(record: JsonRecord | undefined, key: string): string | undefined {
  if (!record) {
    return undefined;
  }
  const value = record[key];
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

export function numberValue(record: JsonRecord | undefined, key: string): number | undefined {
  if (!record) {
    return undefined;
  }
  const value = record[key];
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
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
