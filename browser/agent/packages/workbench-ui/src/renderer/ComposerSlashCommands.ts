import type { KeyboardEvent as ReactKeyboardEvent } from "react";
import type { InitializeResult, RuntimeContext } from "../shared/protocol";

export type ComposerSlashCommandAction =
  | "new-thread"
  | "open-review"
  | "open-files"
  | "open-terminal"
  | "open-project"
  | "no-project"
  | "model"
  | "effort"
  | "settings";
export type ComposerSlashCommandKind = "prompt" | "action";

export type ComposerSlashCommand = {
  id: string;
  name: string;
  title: string;
  description: string;
  tag: string;
  kind: ComposerSlashCommandKind;
  action?: ComposerSlashCommandAction;
  aliases?: string[];
  keywords?: string[];
  disabledReason?: string;
};

export type ComposerSlashDraft = {
  query: string;
  args: string;
};

const COMPOSER_SLASH_COMMAND_LIMIT = 8;
const DEFAULT_REVIEW_SLASH_PROMPT =
  "Review the current code changes (staged, unstaged, and untracked files) and provide prioritized findings.";

export function parseComposerSlashDraft(value: string): ComposerSlashDraft | undefined {
  if (!value.startsWith("/") || value.startsWith("//") || value.includes("\n")) {
    return undefined;
  }
  const body = value.slice(1);
  if (/^\s/.test(body)) {
    return undefined;
  }
  const match = body.match(/^(\S*)(?:\s+(.*))?$/);
  if (!match) {
    return undefined;
  }
  return {
    query: (match[1] ?? "").toLowerCase(),
    args: match[2] ?? ""
  };
}

export function isComposerTextComposing<T extends Element>(event: ReactKeyboardEvent<T>): boolean {
  // Chromium reports keyCode 229 for some active IME key events even when isComposing is unreliable.
  return event.nativeEvent.isComposing || event.keyCode === 229;
}

export function buildComposerSlashCommands({
  activeContext,
  initialized,
  running
}: {
  activeContext?: RuntimeContext;
  initialized?: InitializeResult;
  running: boolean;
}): ComposerSlashCommand[] {
  const needsRuntime = activeContext && initialized ? undefined : "先选择工作区";
  const needsWorkspace = activeContext ? undefined : "先选择工作区";
  const needsIdleThread = running ? "当前任务运行中" : undefined;
  return [
    {
      id: "review",
      name: "review",
      title: "审查当前更改",
      description: "把本地改动整理成审查请求，可继续补充要求",
      tag: "Agent",
      kind: "prompt",
      aliases: ["audit"],
      keywords: ["diff", "changes", "code review", "审查", "检查"],
      disabledReason: needsRuntime
    },
    {
      id: "diff",
      name: "diff",
      title: "打开变更面板",
      description: "查看当前 Git diff 和文件改动",
      tag: "工作区",
      kind: "action",
      action: "open-review",
      aliases: ["changes"],
      keywords: ["review", "git", "diff", "变更"],
      disabledReason: needsWorkspace
    },
    {
      id: "new",
      name: "new",
      title: "新建对话",
      description: "清空当前对话视图，保留所选项目",
      tag: "会话",
      kind: "action",
      action: "new-thread",
      aliases: ["clear"],
      keywords: ["conversation", "thread", "新对话"],
      disabledReason: needsWorkspace ?? needsIdleThread
    },
    {
      id: "terminal",
      name: "terminal",
      title: "打开终端",
      description: "在当前工作区启动 shell",
      tag: "工作区",
      kind: "action",
      action: "open-terminal",
      aliases: ["shell"],
      keywords: ["命令行", "terminal", "shell"],
      disabledReason: needsWorkspace
    },
    {
      id: "files",
      name: "files",
      title: "打开文件",
      description: "浏览当前工作区文件树",
      tag: "工作区",
      kind: "action",
      action: "open-files",
      aliases: ["file", "tree"],
      keywords: ["文件", "浏览", "explorer"],
      disabledReason: needsWorkspace
    },
    {
      id: "project",
      name: "project",
      title: "打开项目",
      description: "选择一个本地文件夹作为工作区",
      tag: "项目",
      kind: "action",
      action: "open-project",
      aliases: ["open"],
      keywords: ["folder", "workspace", "项目"]
    },
    {
      id: "no-project",
      name: "no-project",
      title: "切到临时工作区",
      description: "不绑定项目，直接开始一个临时对话",
      tag: "项目",
      kind: "action",
      action: "no-project",
      aliases: ["scratch", "none"],
      keywords: ["temporary", "临时", "无项目"],
      disabledReason: needsIdleThread
    },
    {
      id: "model",
      name: "model",
      title: "切换模型",
      description: "快速选择 provider、模型和思考强度",
      tag: "配置",
      kind: "action",
      action: "model",
      aliases: ["models"],
      keywords: ["provider", "effort", "thinking", "模型", "思考强度"],
      disabledReason: needsRuntime ?? needsIdleThread
    },
    {
      id: "effort",
      name: "effort",
      title: "调整思考强度",
      description: "切换当前模型支持的 reasoning effort",
      tag: "配置",
      kind: "action",
      action: "effort",
      aliases: ["variants", "thinking"],
      keywords: ["reasoning", "variant", "think", "思考", "强度"],
      disabledReason: needsRuntime ?? needsIdleThread
    },
    {
      id: "settings",
      name: "settings",
      title: "打开设置",
      description: "切换 provider、模型和运行配置",
      tag: "配置",
      kind: "action",
      action: "settings",
      aliases: ["config"],
      keywords: ["provider", "模型", "配置"]
    }
  ];
}

export function filterComposerSlashCommands(commands: ComposerSlashCommand[], query: string): ComposerSlashCommand[] {
  const normalized = query.trim().toLowerCase();
  if (!normalized) {
    return commands.slice(0, COMPOSER_SLASH_COMMAND_LIMIT);
  }
  return commands
    .filter((command) => composerSlashCommandSearchText(command).includes(normalized))
    .slice(0, COMPOSER_SLASH_COMMAND_LIMIT);
}

export function firstEnabledSlashCommandIndex(commands: ComposerSlashCommand[]): number {
  const index = commands.findIndex((command) => !command.disabledReason);
  return index >= 0 ? index : 0;
}

export function nextEnabledSlashCommandIndex(commands: ComposerSlashCommand[], current: number, direction: 1 | -1): number {
  if (commands.length === 0) {
    return 0;
  }
  for (let step = 1; step <= commands.length; step++) {
    const index = (current + direction * step + commands.length) % commands.length;
    if (!commands[index]?.disabledReason) {
      return index;
    }
  }
  return current;
}

function composerSlashCommandSearchText(command: ComposerSlashCommand): string {
  return [command.name, command.title, command.description, command.tag, ...(command.aliases ?? []), ...(command.keywords ?? [])]
    .join(" ")
    .toLowerCase();
}

export function composerSlashPrompt(command: ComposerSlashCommand, args: string): string {
  if (command.id !== "review") {
    return `/${command.name}${args ? ` ${args}` : ""}`;
  }
  const instructions = args.trim();
  if (!instructions) {
    return DEFAULT_REVIEW_SLASH_PROMPT;
  }
  return `${DEFAULT_REVIEW_SLASH_PROMPT}\n\nAdditional review instructions:\n${instructions}`;
}
