<h1 align="center">wuu</h1>

<p align="center">开源、自带 API Key 的 AI Coding Agent —— Go 核心 + 桌面应用 + 可脚本化 CLI，内置多智能体编排能力。</p>

<p align="center">
  <a href="https://github.com/blueberrycongee/wuu/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/blueberrycongee/wuu/ci.yml?branch=main&style=flat-square&label=ci"></a>
  <a href="https://github.com/blueberrycongee/wuu/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/github/license/blueberrycongee/wuu?style=flat-square"></a>
</p>

<p align="center">
  <a href="README.md">English</a> |
  <a href="README_zh.md">简体中文</a> |
  <a href="docs/exec.md">文档</a> |
  <a href="CONTRIBUTING.md">贡献指南</a>
</p>

---

**wuu** 是一个开源的 AI Coding Agent，在本地仓库里处理软件开发任务。它可以阅读和修改文件、运行命令、审查改动、接收文件或截图，并恢复之前的会话——全部通过 BYOK（自带 API Key）模式运行，支持 Anthropic 和任何 OpenAI 兼容的提供商。

除了单轮任务，wuu 还能规划多步工作、委派给专门的子智能体、运行可持久化的工作流、应用任务专属技能，并跨会话记住上下文。桌面应用用于交互式工作，`wuu exec` 则适合脚本、CI 和其他 agent 调用。

## 安装

Wuu 还没有发布 release 二进制包。CLI 可以从源码安装：

```bash
go install github.com/blueberrycongee/wuu/cmd/wuu@latest
```

也可以从本地 checkout 直接运行：

```bash
git clone https://github.com/blueberrycongee/wuu.git
cd wuu
go run ./cmd/wuu --version
```

## 快速开始

```bash
wuu init
wuu exec "描述一下这个仓库"
wuu exec "修复失败的测试"
```

任务需要本地文件时，可以作为附件传入：

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

JSON-RPC 接口详见 [`app-server` 协议](docs/app-server-protocol.md)。

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

项目配置通常放在 `.wuu.json`，全局配置可以放在 `~/.config/wuu/config.json`。

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

## 文档

- [`wuu exec`](docs/exec.md)
- [配置模型与迁移设计](docs/configuration-model-zh.md)
- [`app-server` 协议](docs/app-server-protocol.md)
- [`jsonl-events`](docs/jsonl-events.md)
- [贡献指南](CONTRIBUTING.md)

## 状态

Wuu 还没有到 1.0，正在持续开发中。Release 二进制包还没有发布。接口、配置和桌面端行为都可能继续调整。

## 许可证

MIT
