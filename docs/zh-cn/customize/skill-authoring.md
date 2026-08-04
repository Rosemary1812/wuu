# 编写与安装 Skill

一个 Skill 通常是一个目录，其中包含 `SKILL.md` 和可选的脚本、参考资料或资源文件。
Wuu 在会话启动时发现它们，并在真正使用时才把完整正文加载进上下文。

## 创建最小 Skill

在项目中创建 `.wuu/skills/release-check/SKILL.md`：

```markdown
---
name: release-check
description: 发布前检查版本、构建和发布说明
allowed-tools:
  - read_file
  - grep
  - bash
argument-hint: "[版本号]"
---

# 发布前检查

1. 读取项目发布文档和版本文件。
2. 确认版本号为 ${ARGUMENTS}。
3. 运行文档规定的发布检查。
4. 只报告证据，不要创建标签或发布产物。
```

目录名就是 Skill 的实际名称。为了兼容其他 Agent 工具，使用 1–64 个小写字母、数字和
单连字符，不要以连字符开头或结尾，也不要写连续连字符。

`description` 应说明何时使用和能交付什么。它决定 Agent 是否能从目录中正确选择 Skill。

## 安装位置

Wuu 会扫描以下位置。

### 项目级

- `.wuu/skills/<name>/SKILL.md`
- `.agents/skills/<name>/SKILL.md`
- `.claude/skills/<name>/SKILL.md`
- `.opencode/skills/<name>/SKILL.md`
- `.opencode/skill/<name>/SKILL.md`

从当前工作目录向版本库根目录逐层扫描。同一层中 `.wuu/skills` 优先级最高；更靠近当前
工作目录的定义会覆盖祖先目录中的同名 Skill。

### 用户级

以下按同名覆盖优先级从低到高排列：

- `~/.codex/skills/<name>/SKILL.md`
- `~/.claude/skills/<name>/SKILL.md`
- `~/.agents/skills/<name>/SKILL.md`
- `~/.config/opencode/skills/<name>/SKILL.md`
- `~/.wuu/skills/<name>/SKILL.md`

项目级同名 Skill 覆盖用户级，磁盘上的 Skill 覆盖 Wuu 内置 Skill。原生 Skill 也会
覆盖同名的 `.claude/commands/*.md` 或 `.wuu/commands/*.md` 兼容命令。

Wuu 也接受 skills 根目录下的扁平 `<name>.md` 文件，但目录加 `SKILL.md` 的形式更容易
携带脚本和资料，也更便于跨工具复用。

## 可用 frontmatter

建议优先使用这些当前运行时能够识别和展示的字段：

| 字段 | 作用 |
| --- | --- |
| `name` | 声明名称；目录形式最终仍以目录名为准 |
| `description` | 模型目录中的摘要；为空时只可按名称调用 |
| `when-to-use` / `trigger` | 补充使用时机元数据 |
| `allowed-tools` | 声明需要的工具，并用于当前工具表面的兼容筛选 |
| `user-invocable` | 是否作为用户可调用项展示，默认 `true` |
| `disable-model-invocation` | 为 `true` 时不让模型自行选择 |
| `argument-hint` | 提示调用参数格式 |
| `required-context`、`examples`、`verification-checklist` | 补充目录和工作流元数据 |
| `progressive-disclosure`、`version` | 兼容元数据 |

Wuu 还会解析一些生态兼容字段，但当前不执行其承诺：

- `model` 不会切换会话模型；
- `context` 不会改变内联加载方式；
- `agent` 不会自动创建子 Agent；
- `effort` 不会改变推理强度；
- `paths` 不会按路径自动激活；
- `hooks` 不会注册钩子。

`shell` 也会被解析，但当前 `load_skill` 路径不执行内联 shell，因此它不会改变加载行为。

`wuu skills lint` 会为上面的 `model` 到 `hooks` 字段给出 warning。不要依赖它们控制
现有运行时行为。`shell` 当前不会触发 warning，但正常加载同样不会执行内联命令。

## 参数和资源路径

正文支持以下变量：

- `${ARGUMENTS}`：调用时传入的参数；
- `${CLAUDE_SKILL_DIR}`：Skill 目录；
- `${CLAUDE_SESSION_ID}`：当前会话 ID。

加载结果还会告诉 Agent 这个 Skill 的基准目录，并抽样列出其中的文件。正文中的
`scripts/`、`references/` 等相对路径都应以 Skill 目录为基准。

底层兼容解析器认识以 `!` 开头的行内代码和围栏代码块，但当前产品的 `load_skill`
路径明确关闭了内联 shell 执行，会保留原文。不要依赖这类语法采集动态内容；需要命令
结果时，在工作流正文中明确要求 Agent 使用当前可用工具，并让正常权限规则生效。

## 检查

可以检查一个 Skill、一个 skills 根目录或扁平 Markdown 文件：

```bash
wuu skills lint .wuu/skills/release-check
wuu skills lint .wuu/skills
wuu skills lint --json .wuu/skills
```

- `error` 表示发现过程会丢弃 Skill，或无法读取它的元数据；命令以非零状态退出；
- `warning` 表示仍可加载，但实际行为可能和作者预期不同。

修复检查结果后，在桌面 Skills 目录中刷新并预览正文，再用低风险任务试运行。结构检查
无法验证正文是否可信，也无法验证后续行为是否安全。

## 排错

### Skill 没有出现

确认文件名为 `SKILL.md`、YAML frontmatter 以 `---` 开始、目录名可用，并刷新 Skills
目录。若填写了 `allowed-tools`，还要确认当前会话具备全部声明工具。

### Agent 不会自动选择

填写具体的 `description`，并确认没有设置 `disable-model-invocation: true`。也可以直接用
`/<name> 参数`调用。

### 加载了错误版本

检查是否存在同名定义。项目覆盖用户，更深层项目目录覆盖祖先，同一层的 `.wuu/skills`
覆盖兼容目录。
