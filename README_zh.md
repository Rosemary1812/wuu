<h1 align="center">wuu</h1>

<p align="center">
  <strong>用桌面应用协作，也能用命令行自动化的 AI 编程工作台。</strong>
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

Wuu 把 AI 编程助手放在你的仓库旁边。你可以打开桌面应用和它一起推进任务，也可以在终端、CI 或其他 agent 里调用 `wuu exec` 跑同一套工作流。

它可以阅读和修改文件、运行命令、接收截图和 PDF、审查改动，并在之后恢复项目会话。给 Wuu 一个可验证的信号时，它会更稳：测试、构建命令、截图、日志，或者任何能说明任务是否完成的命令输出。

## 一次典型的使用方式

先让 Wuu 进入仓库，再描述你想完成的改动。Wuu 会先收集上下文，再编辑文件，并用你指定的检查结果继续迭代。会话变长后，可以从桌面应用继续，也可以用 `wuu exec` 恢复。

```bash
wuu init
wuu exec "找出测试失败的原因，修掉根因，然后重新运行测试"
```

任务需要更多上下文时，可以直接传入文件：

```bash
wuu exec --file report.pdf "总结这份 PDF，并更新相关文档"
wuu exec --image screenshot.png "定位这个界面问题，并提出修复方案"
```

自动化场景可以使用 JSONL 输出：

```bash
wuu exec --json "review 当前 diff"
wuu exec resume --last "从上一次失败处继续"
wuu session list --json
```

## 现在适合处理的工作

- 探索陌生仓库，并解释代码之间的关系。
- 完成范围明确的代码修改，然后运行你指定的检查。
- 结合仓库上下文审查本地改动、分支或提交。
- 在后续任务里继续使用已有会话历史。
- 把文件和图片传给 agent，减少手动复制粘贴。
- 为脚本和 CI 输出 JSONL。

## 安装

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

也可以直接运行 npm 包：

```bash
npx @blueberrycongee/wuu@latest --version
```

发布包可以在 [GitHub Releases](https://github.com/blueberrycongee/wuu/releases) 下载。

## 配置模型

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

然后设置对应的 API key：

```bash
export OPENROUTER_API_KEY="..."
export ANTHROPIC_API_KEY="..."
```

## 桌面应用

桌面应用是 Wuu 的主要交互界面。如果你正在本仓库里开发桌面端，可以这样启动：

```bash
cd desktop
npm install
npm run dev
```

`wuu` 二进制也提供桌面 shell 使用的 app-server，后续 shell 和本地工具可以复用同一个运行时。

## 文档

- [`wuu exec`](docs/exec.md)：非交互运行、JSONL 输出、附件、恢复和 review 命令。
- [`app-server` 协议](docs/app-server-protocol.md)：桌面应用和外部 shell 使用的协议。
- [`jsonl-events`](docs/jsonl-events.md)：自动化事件流参考。
- [`CONTRIBUTING.md`](CONTRIBUTING.md)：开发环境和贡献说明。

## 状态

Wuu 还没有到 1.0。桌面应用、自动化入口、模型 provider 和配置格式都还在迭代。

## 许可证

MIT
