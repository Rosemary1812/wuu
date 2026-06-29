<h1 align="center">wuu</h1>

<p align="center">开源的 AI Coding Agent，提供源码运行的桌面应用和可脚本调用的 CLI。</p>

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

Wuu 用来在本地仓库里处理软件开发任务。它可以阅读和修改文件、运行命令、审查改动、接收文件或截图，并恢复之前的会话。

桌面应用目前需要从源码启动；脚本、CI 或其他工具调用用 `wuu exec`。

## 安装

Wuu 还没有发布 npm、Homebrew 或 release 二进制包。CLI 可以从源码安装：

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

Wuu 支持 Anthropic 和 OpenAI 兼容提供商，例如 OpenAI、OpenRouter、one-api、本地网关等。

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
- [`app-server` 协议](docs/app-server-protocol.md)
- [`jsonl-events`](docs/jsonl-events.md)
- [贡献指南](CONTRIBUTING.md)

## 状态

Wuu 还没有到 1.0，正在持续开发中。包管理器安装和 release 二进制包还没有发布。接口、配置和桌面端行为都可能继续调整。

## 许可证

MIT
