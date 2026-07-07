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

除了单轮任务，wuu 还能规划多步工作、委派给专门的子智能体、运行可持久化的工作流、应用任务专属技能，并跨会话记住上下文。桌面应用用于交互式工作，`wuu exec` 则适合脚本、CI 和其他 agent 调用。

## 从这里开始

| 你想要... | 前往 |
|---|---|
| 安装并跑通第一个任务 | [安装](#安装) 和 [快速开始](#快速开始) |
| 使用桌面应用 | [桌面应用](#桌面应用) |
| 在脚本、CI 或其他 agent 中调用 wuu | [CLI 和自动化](#cli-和自动化) 和 [`docs/exec.md`](docs/exec.md) |
| 接入模型提供商（Anthropic、OpenAI 兼容、本地） | [模型提供商](#模型提供商) |
| 理解或嵌入 Go 核心 | [架构](#架构) 和 [`app-server` 协议](docs/app-server-protocol.md) |
| 参与贡献 | [贡献指南](CONTRIBUTING.md) |

## 动态

- **2026-07-01** 发布 **v0.1.0** —— 第一个版本化里程碑：MIT 许可证、贡献指南、安全策略和开源治理文件全部就位。详见 [CHANGELOG](CHANGELOG.md)。

## 为什么选 wuu

- **BYOK，不锁定** —— 自带 API Key，支持 Anthropic 和任何 OpenAI 兼容端点，包括本地网关。
- **一个核心，多个 Shell** —— Go 核心通过 `wuu app-server` 提供 JSON-RPC 接口；桌面应用是第一个 Shell，编辑器插件可以直接复用同一个核心，无需 fork。
- **编排能力内置** —— 子智能体、可持久化工作流、技能、持久记忆和定时任务都是运行时的一部分，不是外挂。
- **为脚本化而设计** —— `wuu exec` 输出流式 JSONL，CI 任务、review 机器人和其他 agent 都可以编程式驱动它。
- **会话可持久** —— 恢复之前的对话、从检查点 fork、跨会话保留上下文。

## 实战对比

在本仓库的一个真实前端 bug 上，各方拿到的是完全一致的初始提示词——一份高度模糊化的描述，从定位问题到解决全部交由 agent 自主完成。三个运行 MiniMax-M3 的 wuu 群聊 agent 自主定位问题并交付了可用修复，API 费用 $2.66；同样的任务，单 agent 的 Claude Fable 5 修复花费约 $50，Claude Opus 4.8 花费约 $20 仍未修复。多智能体协作让便宜模型以约二十分之一的成本拿到了同样的结果。

## 安装

> [!IMPORTANT]
> wuu 还未到 1.0，Release 二进制包尚未发布——目前用 `go install` 从源码安装是最可靠的方式。安装脚本会拉取 GitHub 上最新的 tagged release，等 release 发布后即可使用。接口、配置和桌面端行为都可能继续调整。

选择**一种**安装方式：

**用 Go 从源码安装**

```bash
go install github.com/blueberrycongee/wuu/cmd/wuu@latest
```

**一键安装脚本**（下载 release 二进制包）

```bash
curl -fsSL https://raw.githubusercontent.com/blueberrycongee/wuu/main/install.sh | sh
```

安装脚本默认下载到 `~/.local/bin`。如需自定义安装路径：

```bash
INSTALL_DIR=/usr/local/bin sh install.sh
```

**从本地 checkout 直接运行**

```bash
git clone https://github.com/blueberrycongee/wuu.git
cd wuu
go run ./cmd/wuu --version
```

验证安装：

```bash
wuu --version
```

## 快速开始

**1. 初始化**

```bash
wuu init
```

**2. 运行第一个任务**

```bash
wuu exec "描述一下这个仓库"
wuu exec "修复失败的测试"
```

**3. 任务需要本地文件时，作为附件传入**

```bash
wuu exec --file report.pdf "总结这个 PDF"
wuu exec --image screenshot.png "找出这个界面的问题"
```

**4. 恢复或查看会话**

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
- **子智能体** — 委派给专门的智能体（规划器、执行器、审查器、调试器、QA 等），支持并行或隔离工作
- **工作流** — 可持久化的多步运行，包含阶段、工作进程派生和恢复
- **技能** — 针对规划、审查、前端设计等特定任务的指令集
- **持久记忆** — 智能体档案可跨会话记住偏好和上下文
- **定时任务** — 按 cron 计划运行提示或工作流

**提供商与集成**
- **BYOK / 多提供商** — 自带 API Key；支持 Anthropic 和 OpenAI 兼容网关（OpenAI、OpenRouter、one-api、本地等）
- **JSONL 输出** — 可脚本化、可流式的输出，适合 CI 和其他 agent
- **桌面应用** — 源码运行的 UI，与 CLI 配合使用

## 架构

Wuu 分为可复用的 **Go 核心** 和轻量的 **Shell**：

- **Go 核心**（`internal/`、`cmd/wuu/`）提供智能体运行时、提供商、工具循环、会话和配置。它通过 `wuu app-server` 作为子进程运行。
- **当前 Shell** 是 `desktop/` 中的 Electron 桌面应用，负责派生核心并管理 UI 和原生集成。
- **未来的 Shell**（VS Code 插件、JetBrains 插件等）可以通过派生 `wuu app-server` 来复用同一个核心——无需导入或 fork Go 代码。

> [!TIP]
> 想构建新的 Shell 或集成？从 [`app-server` 协议](docs/app-server-protocol.md) 开始——它完整记录了桌面应用所使用的 JSON-RPC 接口。

## 桌面应用

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

项目配置通常放在 `.wuu.json`，全局配置放在 `~/.wuu/config.json`（设 `WUU_HOME` 可整体搬走该目录；旧位置 `~/.config/wuu/config.json` 仍会被读取以向后兼容）。

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
- 了解配置模型与迁移设计：[配置模型（中文）](docs/configuration-model-zh.md)
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

## Star History

<div align="center">
  <a href="https://star-history.com/#blueberrycongee/wuu&Date">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=blueberrycongee/wuu&type=Date&theme=dark" />
      <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=blueberrycongee/wuu&type=Date" />
      <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=blueberrycongee/wuu&type=Date" />
    </picture>
  </a>
</div>

## 许可证

[MIT](LICENSE)
