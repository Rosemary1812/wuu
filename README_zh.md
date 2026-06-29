<h1 align="center">wuu</h1>

<p align="center">
  <strong>桌面优先的开源 AI 编程助手，面向真实代码仓库。</strong>
</p>

<p align="center">
  你可以在桌面应用里交互式使用 Wuu，也可以通过 <code>wuu exec</code> 把同一套能力接入脚本、CI 和其他工具。
</p>

<p align="center">
  <a href="https://github.com/blueberrycongee/wuu/releases"><img alt="Release" src="https://img.shields.io/github/v/release/blueberrycongee/wuu?style=flat-square"></a>
  <a href="https://github.com/blueberrycongee/wuu/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/blueberrycongee/wuu/ci.yml?branch=main&style=flat-square&label=ci"></a>
  <a href="https://www.npmjs.com/package/@blueberrycongee/wuu"><img alt="npm" src="https://img.shields.io/npm/v/@blueberrycongee/wuu?style=flat-square"></a>
  <a href="https://github.com/blueberrycongee/wuu/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/github/license/blueberrycongee/wuu?style=flat-square"></a>
</p>

<p align="center">
  <a href="README.md">English</a> |
  <a href="README_zh.md">简体中文</a> |
  <a href="docs/exec.md">Exec 文档</a> |
  <a href="CONTRIBUTING.md">贡献指南</a>
</p>

---

## Wuu 是什么？

Wuu 是一个开源 AI 编程助手，包含桌面界面和适合自动化调用的命令行入口。它可以阅读和修改文件、运行检查、审查改动、接收本地文件或截图，并在之后恢复项目会话。

它更适合直接进入真实仓库工作，而不是停留在一个和项目脱节的聊天窗口里。

## 功能

- **桌面优先**：用 Electron 桌面应用处理日常交互式开发。
- **可脚本调用**：用 `wuu exec` 接入 shell 脚本、CI 或其他 agent。
- **自带模型选择权**：支持 Anthropic 和 OpenAI 兼容接口，例如 OpenAI、OpenRouter、one-api、本地网关等。
- **项目会话**：支持列出、恢复、fork、搜索、归档和删除会话。
- **仓库内工作**：在当前工作区里阅读、编辑、搜索、打补丁和运行命令。
- **附件输入**：可以把 PDF、文本文件和截图交给 Wuu 处理。
- **机器可读输出**：`wuu exec --json` 可以输出 JSONL，方便自动化消费。

## 安装

任选一种方式安装 `wuu` 命令：

```bash
# Homebrew
brew install blueberrycongee/tap/wuu

# npm
npm install -g @blueberrycongee/wuu

# 安装脚本
curl -fsSL https://raw.githubusercontent.com/blueberrycongee/wuu/main/install.sh | sh

# 从源码安装
go install github.com/blueberrycongee/wuu/cmd/wuu@latest
```

也可以不全局安装，直接通过 npm 运行：

```bash
npx @blueberrycongee/wuu@latest --version
```

## 快速开始

先创建项目配置，然后让 Wuu 在当前仓库里工作：

```bash
wuu init
wuu exec "描述一下这个仓库"
wuu exec "修复失败的测试并验证"
```

任务需要本地文件或截图时，可以作为附件传入：

```bash
wuu exec --file report.pdf "总结这个 PDF"
wuu exec --image screenshot.png "找出这个界面的问题"
```

恢复或查看之前的工作：

```bash
wuu exec resume --last "继续上一个任务"
wuu session list --json
```

## 桌面应用

桌面应用是 Wuu 的主要交互体验。如果你正在本仓库里开发桌面端，可以这样启动：

```bash
cd desktop
npm install
npm run dev
```

已安装的 `wuu` 命令仍然适合非交互任务、自动化流程，或作为桌面 shell 使用的后端进程。

## 配置

Wuu 会读取项目级 `.wuu.json`，也会读取全局配置 `~/.config/wuu/config.json`。

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

然后在 shell 里设置对应的 API key：

```bash
export OPENROUTER_API_KEY="..."
export ANTHROPIC_API_KEY="..."
```

## 文档

- [`wuu exec`](docs/exec.md)：自动化入口、JSONL 输出、附件、恢复和 review 命令。
- [`app-server` 协议](docs/app-server-protocol.md)：桌面应用和外部 shell 使用的协议。
- [`jsonl-events`](docs/jsonl-events.md)：自动化事件流参考。
- [`CONTRIBUTING.md`](CONTRIBUTING.md)：开发环境和贡献说明。

## 项目状态

Wuu 还没有到 1.0，正在快速迭代。桌面应用、自动化入口、模型 provider 和配置方式都可能继续调整。

## 许可证

MIT
