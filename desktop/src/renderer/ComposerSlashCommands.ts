import type { KeyboardEvent as ReactKeyboardEvent } from "react";
import type { InitializeResult, RuntimeContext, SkillSummary } from "../shared/protocol";

export type ComposerSlashCommandAction =
  | "new-thread"
  | "open-review"
  | "open-skills"
  | "open-files"
  | "open-terminal"
  | "open-project"
  | "no-project"
  | "context"
  | "instructions"
  | "model"
  | "fast"
  | "effort"
  | "settings";
export type ComposerSlashCommandKind = "prompt" | "action" | "skill";

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
  argumentHint?: string;
  disabledReason?: string;
};

export type ComposerSlashDraft = {
  query: string;
  args: string;
};

export type ComposerFastModelTarget = {
  provider: string;
  model: string;
  current: boolean;
};

const COMPOSER_SLASH_COMMAND_LIMIT = 8;

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
  running,
  skills = []
}: {
  activeContext?: RuntimeContext;
  initialized?: InitializeResult;
  running: boolean;
  skills?: SkillSummary[];
}): ComposerSlashCommand[] {
  const needsRuntime = activeContext && initialized ? undefined : "先选择工作区";
  const needsWorkspace = activeContext ? undefined : "先选择工作区";
  const needsIdleThread = running ? "当前任务运行中" : undefined;
  const fastTarget = runtimeFastModelTarget(initialized);
  const commands: ComposerSlashCommand[] = [
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
      id: "debug",
      name: "debug",
      title: "调查问题",
      description: "复现或定位证据，先找根因再修复",
      tag: "Agent",
      kind: "prompt",
      aliases: ["investigate", "diagnose"],
      keywords: ["bug", "error", "failure", "失败", "报错", "排查", "根因"],
      disabledReason: needsRuntime
    },
    {
      id: "fix",
      name: "fix",
      title: "修复问题",
      description: "读取相关代码，做最小且完整的修复",
      tag: "Agent",
      kind: "prompt",
      aliases: ["repair"],
      keywords: ["bug", "issue", "修复", "改掉", "问题"],
      disabledReason: needsRuntime
    },
    {
      id: "helpme",
      name: "helpme",
      title: "HelpMe 求助",
      description: "让 fresh 子 agent 重新理解，并自动压缩上下文",
      tag: "Agent",
      kind: "prompt",
      aliases: ["rescue", "handoff"],
      keywords: ["stuck", "retry", "rescue", "求助", "卡住", "魂穿", "交接"],
      disabledReason: needsRuntime
    },
    {
      id: "test",
      name: "test",
      title: "补测试",
      description: "为当前改动补真实行为测试并运行验证",
      tag: "Agent",
      kind: "prompt",
      aliases: ["tests"],
      keywords: ["unit", "e2e", "coverage", "测试", "验证"],
      disabledReason: needsRuntime
    },
    {
      id: "explain",
      name: "explain",
      title: "解释代码或错误",
      description: "结合文件和运行证据说明当前行为",
      tag: "Agent",
      kind: "prompt",
      aliases: ["why"],
      keywords: ["explain", "understand", "why", "解释", "说明", "为什么"],
      disabledReason: needsRuntime
    },
    {
      id: "skills",
      name: "skills",
      title: "浏览 Skills",
      description: "查看可用 Skills，并生成可补充参数的草稿",
      tag: "Skill",
      kind: "action",
      action: "open-skills",
      aliases: ["skill"],
      keywords: ["skills", "skill", "技能", "能力"],
      disabledReason: needsRuntime
    },
    {
      id: "context",
      name: "context",
      title: "查看上下文组成",
      description: "打开最近一次模型请求的上下文组成视图",
      tag: "视图",
      kind: "action",
      action: "context",
      aliases: ["ctx"],
      keywords: ["context", "tokens", "cache", "上下文", "缓存"],
      disabledReason: needsRuntime
    },
    {
      id: "instructions",
      name: "instructions",
      title: "查看指令文件",
      description: "查看已加载的 AGENTS.md / CLAUDE.md 指令文件",
      tag: "视图",
      kind: "action",
      action: "instructions",
      aliases: ["memory", "agents"],
      keywords: ["instructions", "memory", "agents", "claude", "指令", "记忆"],
      disabledReason: needsRuntime
    },
    {
      id: "commit",
      name: "commit",
      title: "提交当前改动",
      description: "检查本地改动，验证后创建一个原子提交",
      tag: "工作流",
      kind: "prompt",
      aliases: ["save"],
      keywords: ["git", "commit", "提交", "保存"],
      disabledReason: needsRuntime ?? needsIdleThread
    },
    {
      id: "pr",
      name: "pr",
      title: "准备 Pull Request",
      description: "整理说明和验证，确认就绪后创建 PR",
      tag: "工作流",
      kind: "prompt",
      aliases: ["pull-request", "pullrequest"],
      keywords: ["github", "pull request", "merge request", "pr", "合并请求"],
      disabledReason: needsRuntime ?? needsIdleThread
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
      id: "compact",
      name: "compact",
      title: "压缩上下文",
      description: "把较早的对话折叠成摘要，释放上下文窗口",
      tag: "会话",
      kind: "prompt",
      aliases: ["compress"],
      keywords: ["compact", "context", "summary", "压缩", "上下文", "摘要", "瘦身"],
      disabledReason: needsRuntime
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
      description: "快速选择 provider、模型和参数档位",
      tag: "配置",
      kind: "action",
      action: "model",
      aliases: ["models"],
      keywords: ["provider", "effort", "variant", "模型", "参数档位"],
      disabledReason: needsRuntime ?? needsIdleThread
    },
    ...(fastTarget
      ? [
          {
            id: "fast",
            name: "fast",
            title: "切换快速模式",
            description: fastTarget.current ? "当前模型已经是 fast mode" : "切到当前模型的 fast mode",
            tag: "配置",
            kind: "action",
            action: "fast",
            aliases: ["quick"],
            keywords: ["fast", "priority", "快速", "高速"],
            disabledReason: needsRuntime ?? needsIdleThread ?? (fastTarget.current ? "当前已是快速模式" : undefined)
          } satisfies ComposerSlashCommand
        ]
      : []),
    {
      id: "effort",
      name: "effort",
      title: "调整模型档位",
      description: "切换当前模型支持的 variant / reasoning effort",
      tag: "配置",
      kind: "action",
      action: "effort",
      aliases: ["variant", "variants"],
      keywords: ["reasoning", "effort", "variant", "思考", "强度"],
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
  return [...commands, ...buildSkillSlashCommands(skills, needsRuntime)];
}

export function runtimeFastModelTarget(initialized?: InitializeResult): ComposerFastModelTarget | undefined {
  if (!initialized) {
    return undefined;
  }
  const provider = initialized.providers?.find((item) => item.name === initialized.provider);
  if (!provider) {
    return undefined;
  }
  const currentModel = initialized.model.trim();
  if (!currentModel) {
    return undefined;
  }
  const currentFast = currentModel.toLowerCase().endsWith("-fast");
  const baseModel = currentFast ? currentModel.slice(0, -"-fast".length) : currentModel;
  const fastModel = `${baseModel}-fast`;
  const hasFastModel = provider.models?.some((model) => model.id === fastModel);
  if (!hasFastModel && !currentFast) {
    return undefined;
  }
  return {
    provider: provider.name,
    model: currentFast ? currentModel : fastModel,
    current: currentFast
  };
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
  const instructions = args.trim();
  return `/${command.name}${instructions ? ` ${instructions}` : " "}`;
}

function buildSkillSlashCommands(skills: SkillSummary[], disabledReason?: string): ComposerSlashCommand[] {
  const seen = new Set<string>();
  const out: ComposerSlashCommand[] = [];
  for (const skill of [...skills].sort(compareSkillSummaries)) {
    const name = skill.name.trim();
    if (!skill.user_invocable || !name || /\s/.test(name)) {
      continue;
    }
    const key = name.toLowerCase();
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    out.push({
      id: `skill:${key}`,
      name,
      title: `/${name}`,
      description: skill.description || skill.when_to_use || skill.trigger_condition || "使用这个 Skill",
      tag: "Skill",
      kind: "skill",
      aliases: skill.examples?.slice(0, 3),
      keywords: [
        "skill",
        "skills",
        "技能",
        skill.source,
        skill.when_to_use,
        skill.trigger_condition,
        skill.argument_hint,
        skill.model,
        skill.context,
        skill.agent,
        ...(skill.paths ?? [])
      ].filter((value): value is string => Boolean(value)),
      argumentHint: skill.argument_hint,
      disabledReason
    });
  }
  return out;
}

function compareSkillSummaries(left: SkillSummary, right: SkillSummary): number {
  const sourceDelta = sourceRank(left.source) - sourceRank(right.source);
  if (sourceDelta !== 0) {
    return sourceDelta;
  }
  return left.name.localeCompare(right.name);
}

function sourceRank(source: string): number {
  switch (source) {
    case "project":
      return 0;
    case "user":
      return 1;
    case "bundled":
      return 2;
    default:
      return 3;
  }
}
