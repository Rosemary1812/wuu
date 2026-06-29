# wuu

Wuu 是一个桌面优先的 AI 编程伙伴。它可以在你的真实项目里阅读代码、理解上下文、修改文件、运行检查，并把过程保存在会话里，方便之后继续。

Wuu is a desktop-first AI coding companion. It works inside your real project: reading code, understanding context, editing files, running checks, and keeping the session easy to continue later.

名字来自作者 Wu，也带一点小小的惊喜感：希望有一天它好用到让开发者发出 "wuuuuu!"。

The name comes from Wu, with a little bit of delight baked in: the goal is to make coding feel good enough to go "wuuuuu!".

## 中文

### Wuu 适合做什么

- 理解一个陌生仓库，并解释它是怎么组织的。
- 修 bug、改功能、补测试，完成后说明改了什么。
- 根据截图、日志、PDF 或现有代码继续推进工作。
- 审查本地改动或 PR，优先指出真正会影响结果的问题。
- 在桌面应用里做交互式开发，也可以用命令行把同样的能力接到脚本或 CI。

### 安装

```bash
# Homebrew
brew install blueberrycongee/tap/wuu

# 脚本安装
curl -fsSL https://raw.githubusercontent.com/blueberrycongee/wuu/main/install.sh | sh

# npm
npx wuu@latest

# 从源码安装
go install github.com/blueberrycongee/wuu/cmd/wuu@latest
```

### 快速开始

```bash
wuu init
wuu exec "描述一下这个仓库"
wuu exec --file report.pdf "总结这个 PDF"
wuu exec --image screenshot.png "找出这个界面的问题"
```

交互式工作适合放在桌面应用里；脚本、CI 或其他自动化流程适合使用 `wuu exec`。

如果你正在本仓库里开发桌面端，可以这样启动本地应用：

```bash
cd desktop
npm install
npm run dev
```

### 配置模型

Wuu 支持 OpenAI 兼容接口和 Anthropic。项目级配置通常放在 `.wuu.json`，全局配置可以放在 `~/.config/wuu/config.json`。

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

把对应的 API key 放进环境变量后就可以使用：

```bash
export OPENROUTER_API_KEY="..."
export ANTHROPIC_API_KEY="..."
```

### 常用命令

```bash
wuu exec "修复失败的测试并验证"
wuu exec --json "review this PR"
wuu exec resume --last "继续刚才的任务"
wuu session list --json
```

### 当前状态

Wuu 仍在快速迭代中。它的重点不是做一个只会聊天的窗口，而是成为一个能真正进入项目、理解上下文、完成修改、留下记录的编程伙伴。

## English

### What Wuu Is For

- Understand an unfamiliar repository and explain how it is organized.
- Fix bugs, build features, add tests, and report what changed.
- Continue work from screenshots, logs, PDFs, or existing code.
- Review local changes or PRs, focusing on issues that can affect the result.
- Work interactively in the desktop app, or use the same agent from scripts and CI.

### Install

```bash
# Homebrew
brew install blueberrycongee/tap/wuu

# Shell script
curl -fsSL https://raw.githubusercontent.com/blueberrycongee/wuu/main/install.sh | sh

# npm
npx wuu@latest

# From source
go install github.com/blueberrycongee/wuu/cmd/wuu@latest
```

### Quick Start

```bash
wuu init
wuu exec "describe this repo"
wuu exec --file report.pdf "summarize this PDF"
wuu exec --image screenshot.png "find the UI issue"
```

Use the desktop app for interactive work. Use `wuu exec` when you want scripts, CI, or another agent to call Wuu directly.

If you are developing the desktop app from this repository, start it locally with:

```bash
cd desktop
npm install
npm run dev
```

### Configure a Model

Wuu supports OpenAI-compatible APIs and Anthropic. Project config usually lives in `.wuu.json`; global config can live in `~/.config/wuu/config.json`.

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

Set the matching API key in your environment:

```bash
export OPENROUTER_API_KEY="..."
export ANTHROPIC_API_KEY="..."
```

### Useful Commands

```bash
wuu exec "fix the failing test and verify it"
wuu exec --json "review this PR"
wuu exec resume --last "continue the previous task"
wuu session list --json
```

### Status

Wuu is moving quickly. The goal is not just another chat window, but a coding companion that can enter a project, understand the context, make changes, and leave a useful trail behind.

## License

MIT
