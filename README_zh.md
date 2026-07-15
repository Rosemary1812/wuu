<h1 align="center">wuu</h1>

<p align="center">开源、自带 API Key 的 AI Coding Agent —— Go 核心 + 桌面应用 + 可脚本化 CLI，内置多智能体编排能力。</p>

<div align="center">
  <p>
    <a href="README.md">English</a> |
    <a href="README_zh.md">简体中文</a>
  </p>
  <p>
    <a href="https://github.com/blueberrycongee/wuu/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/blueberrycongee/wuu/ci.yml?branch=main&style=flat-square&label=ci"></a>
    <a href="https://github.com/blueberrycongee/wuu/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/github/license/blueberrycongee/wuu?style=flat-square"></a>
    <a href="https://github.com/blueberrycongee/wuu/blob/main/go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/blueberrycongee/wuu?style=flat-square"></a>
    <a href="https://github.com/blueberrycongee/wuu/graphs/commit-activity"><img alt="Commit activity" src="https://img.shields.io/github/commit-activity/m/blueberrycongee/wuu?style=flat-square"></a>
  </p>
</div>

---

<img width="2272" height="2494" alt="wuu 桌面应用" src="https://github.com/user-attachments/assets/2d9030aa-ca03-42b1-9333-f79cc5aff95b" />

**wuu** 是一个开源的 AI Coding Agent，在本地仓库里处理软件开发任务。它可以阅读和修改文件、运行命令、审查改动、接收文件或截图，并恢复之前的会话——全部通过 BYOK（自带 API Key）模式运行，支持 Anthropic 和任何 OpenAI 兼容的提供商。

除了单轮任务，wuu 还能规划多步工作、委派给专门的子智能体、应用任务专属技能，并跨会话记住上下文。桌面应用用于交互式工作，`wuu exec` 则适合脚本、CI 和其他 agent 调用。

> [!WARNING]
> **项目状态：** 移动端目前尚未正式推出。wuu 仍处于 1.0 之前的高速迭代阶段，不同版本之间的功能、接口和行为可能发生变化。如果你需要稳定、可用于生产环境的工具，请在采用前谨慎评估。

## 从这里开始

| 你想要... | 前往 |
|---|---|
| 安装并跑通第一个任务 | [安装](#安装) 和 [快速开始](#快速开始) |
| 使用桌面应用 | [桌面应用](#桌面应用) |
| 在脚本、CI 或其他 agent 中调用 wuu | [CLI 和自动化](#cli-和自动化) 和 [`docs/exec.md`](docs/exec.md) |
| 接入模型提供商（Anthropic、OpenAI 兼容、本地） | [模型提供商](#模型提供商) |
| 理解或嵌入 Go 核心 | [架构](#架构) 和 [`app-server` 协议](docs/app-server-protocol.md) |
| 参与贡献 | [贡献指南](CONTRIBUTING.md) |
| 了解安全和信任边界 | [安全模型](docs/security-model.md) |
| 查看可复现的公开评测 | [公开评测记录](evals/) |

## 动态

- **CLI 和桌面安装包** —— 带标签的 GitHub Release 同时提供经过校验的 macOS/Linux CLI 压缩包和未签名的 macOS Electron 预览版。
- **2026-07-10** 发布 **v0.1.0** —— 第一个打包桌面端里程碑：GitHub Releases 提供未签名 macOS Electron 预览包，同时开源治理文件就位。详见 [CHANGELOG](CHANGELOG.md)。

## 为什么选 wuu

- **BYOK，不锁定** —— 自带 API Key，支持 Anthropic 和任何 OpenAI 兼容端点，包括本地网关。
- **一个核心，多个 Shell** —— Go 核心通过 `wuu app-server` 提供 JSON-RPC 接口；桌面应用是第一个 Shell，编辑器插件可以直接复用同一个核心，无需 fork。
- **编排能力内置** —— 子智能体、可持久化目标、技能、持久记忆和定时任务都是运行时的一部分，不是外挂。
- **为脚本化而设计** —— `wuu exec` 输出流式 JSONL，CI 任务、review 机器人和其他 agent 都可以编程式驱动它。
- **会话可持久** —— 恢复之前的对话、从检查点 fork、跨会话保留上下文。

## 安装

> [!IMPORTANT]
> wuu 还未到 1.0。带标签的 GitHub Release 同时包含 CLI 压缩包和未签名的 macOS Electron 桌面预览版。macOS 可能会拦截桌面应用，确认信任下载来源后需要手动移除 quarantine 标记。

选择**一种**安装方式：

**macOS 桌面安装包**（未签名）

从 [GitHub Releases](https://github.com/blueberrycongee/wuu/releases)
下载 `wuu-<version>-mac-arm64.dmg` 或 `wuu-<version>-mac-arm64.zip`。
把 `wuu.app` 放到 `/Applications` 后，macOS 可能因为应用未签名、未公证而阻止打开。
如果 macOS 提示应用无法打开，复制运行这条命令：

```bash
xattr -dr com.apple.quarantine /Applications/wuu.app && open /Applications/wuu.app
```

**用 Go 从源码安装 CLI**

```bash
go install github.com/blueberrycongee/wuu/cmd/wuu@latest
```

也可以安装最新、带校验的 Release 压缩包：

```bash
curl -fsSL https://raw.githubusercontent.com/blueberrycongee/wuu/main/install.sh | sh
```

验证安装：

```bash
wuu --version
```

**从本地 checkout 直接运行**

```bash
git clone https://github.com/blueberrycongee/wuu.git
cd wuu
go run ./cmd/wuu --version
```

## 快速开始

**桌面端**

打开 `wuu.app`，选择一个本地项目文件夹，然后在输入框里开始任务。

**CLI 和自动化**

先初始化一次：

```bash
wuu init
```

该命令会在 `~/.wuu/config.json`（或 `WUU_HOME/config.json`）创建用户配置，
不会把提供商连接和凭据写入项目目录。

运行第一个任务：

```bash
wuu exec "描述一下这个仓库"
wuu exec "修复失败的测试"
```

任务需要本地文件时，作为附件传入：

```bash
wuu exec --file report.pdf "总结这个 PDF"
wuu exec --image screenshot.png "找出这个界面的问题"
```

恢复或查看会话：

```bash
wuu exec resume --last "继续"
wuu session list --json
```

## 功能

**仓库操作**
- **文件操作** — 读取、编辑和检查工作仓库中的文件
- **命令执行** — 运行命令、捕获输出、在失败时迭代
- **附件** — 通过 `--file` 和 `--image` 将本地文件和截图直接传入对话
- **会话** — 恢复之前的对话、列出历史、从检查点 fork

**智能体编排**
- **子智能体** — 委派给子智能体（全新上下文的通用智能体、worktree 隔离的执行器、或继承上下文的分身），支持并行或隔离工作
- **可持久化目标** — 跨上下文丢失仍能存活的长期目标，并可在不同会话中恢复
- **技能** — 针对规划、审查、前端设计等特定任务的指令集
- **持久记忆** — 智能体档案可跨会话记住偏好和上下文
- **定时任务** — 按 cron 计划运行提示

**提供商与集成**
- **BYOK / 多提供商** — 自带 API Key；支持 Anthropic 和 OpenAI 兼容网关（OpenAI、OpenRouter、one-api、本地等）
- **JSONL 输出** — 可脚本化、可流式的输出，适合 CI 和其他 agent
- **桌面应用** — 打包好的 macOS Electron 应用，也可以从源码运行，与 CLI 配合使用

## 架构

Wuu 分为可复用的 **Go 核心** 和轻量的 **Shell**：

- **Go 核心**（`internal/`、`cmd/wuu/`）提供智能体运行时、提供商、工具循环、会话和配置。它通过 `wuu app-server` 作为子进程运行。
- **当前 Shell** 是 `desktop/` 中的 Electron 桌面应用，负责派生核心并管理 UI 和原生集成。
- **未来的 Shell**（VS Code 插件、JetBrains 插件等）可以通过派生 `wuu app-server` 来复用同一个核心——无需导入或 fork Go 代码。

> [!TIP]
> 想构建新的 Shell 或集成？从 [`app-server` 协议](docs/app-server-protocol.md) 开始——它完整记录了桌面应用所使用的 JSON-RPC 接口。

## 桌面应用

第一版桌面安装包是 [GitHub Releases](https://github.com/blueberrycongee/wuu/releases) 上的 macOS Electron 应用。
它目前是未签名版本；Gatekeeper 的 quarantine 处理方式见安装章节。

桌面端代码在 `desktop/`。从源码启动：

```bash
cd desktop
npm install
npm run dev
```

## CLI 和自动化

`wuu exec` 是非交互入口，适合脚本、CI、review 任务和其他 agent 调用。

```bash
wuu exec --json "review 当前 diff"
wuu exec --file plan.md "实现这个计划"
wuu exec review --uncommitted
```

JSONL 输出、附件、恢复、fork、review 和自动化选项见 [`docs/exec.md`](docs/exec.md)。

## 模型提供商

Wuu 支持 Anthropic 和 OpenAI 兼容提供商，例如 OpenAI、OpenRouter、one-api、本地网关等。自带 API Key——设置对应的环境变量，将 wuu 指向任意兼容端点即可。

提供商选择、模型、端点、凭据来源、全局记忆设置和权限模式都放在用户配置
`~/.wuu/config.json` 中。设 `WUU_HOME` 可以整体搬走该目录；旧位置
`~/.config/wuu/config.json` 仍会迁移并读取，以保持兼容。

项目文件（`.wuu.json`、`wuu.json`、`.wuu/settings.json` 和
`.wuu/settings.local.json`）可以添加提示词等项目行为，但正常启动会忽略其中的
提供商选择与定义、角色模型选择、记忆发现设置和权限模式。这样仓库不能把用户凭据
重定向到其他端点、把后台任务切到另一个已配置提供商，或指定工作区外的任意文件。
Wuu 的全局记忆仍位于工作区外、用户控制的 Wuu 主目录中，并可正常读取和更新。

```json
{
  "default_provider": "openrouter",
  "providers": {
    "openrouter": {
      "type": "openai-compatible",
      "base_url": "https://openrouter.ai/api/v1",
      "api_key_env": "OPENROUTER_API_KEY",
      "model": "openai/gpt-4.1-mini"
    },
    "anthropic": {
      "type": "anthropic",
      "api_key_env": "ANTHROPIC_API_KEY",
      "model": "claude-sonnet-4-20250514"
    }
  }
}
```

然后设置对应的环境变量：

```bash
export OPENROUTER_API_KEY="..."
export ANTHROPIC_API_KEY="..."
```

换用其他提供商时，配置结构相同：

| 替换项 | 位置 |
|---|---|
| 提供商配置键 | `providers.<provider>` |
| 提供商类型 | `providers.<provider>.type`（`anthropic` 或 `openai-compatible`） |
| 端点 URL（按需） | `providers.<provider>.base_url` |
| API Key 环境变量名 | `providers.<provider>.api_key_env` |
| 模型 ID | `providers.<provider>.model` |

## 文档

- 在脚本、CI 或其他 agent 中调用 wuu：[`wuu exec`](docs/exec.md)
- 解析流式输出：[JSONL 事件](docs/jsonl-events.md)
- 将核心嵌入新的 Shell：[`app-server` 协议](docs/app-server-protocol.md)
- 消费 Claude Code 兼容的流式输出：[cc-stream-json](docs/compat/cc-stream-json.md)
- 了解配置加载和自动化入口：[`wuu exec`](docs/exec.md)
- 搭建开发环境：[贡献指南](CONTRIBUTING.md)

## 参与贡献

欢迎 PR！环境搭建、review 流程和贡献规范见 [CONTRIBUTING.md](CONTRIBUTING.md)，安全漏洞报告方式见 [SECURITY.md](SECURITY.md)。

Wuu 还未到 1.0，正在持续开发中——遇到问题欢迎[提 issue](https://github.com/blueberrycongee/wuu/issues)。

## 致谢

wuu 的设计深度借鉴并受益于以下项目。它们在智能体运行时、工具循环、多智能体编排和开发者体验方面的工作，影响了 wuu 许多架构决策与权衡取舍。

- [Codex](https://github.com/openai/codex) — OpenAI 的 coding agent
- [OpenCode](https://github.com/sst/opencode) — 开源的终端 coding agent
- [pi](https://github.com/badlogic/pi-mono) — Mario Zechner 的极简 AI agent 工具集
- [Kimi Code](https://github.com/MoonshotAI/kimi-cli) — 月之暗面(Moonshot AI)的 coding agent

感谢这些项目背后的团队和社区，正是你们的实践与思考让 wuu 成为可能。

## 许可证

[MIT](LICENSE)
