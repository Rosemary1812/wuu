<h1 align="center">wuu</h1>

<p align="center"><strong>把 Coding Agent 放进桌面工作区。</strong></p>

<p align="center">开源 · macOS · 自选模型</p>

<div align="center">
  <p>
    <a href="README.md">English</a> |
    <a href="README_zh.md">简体中文</a>
  </p>
  <p>
    <a href="https://github.com/blueberrycongee/wuu/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/blueberrycongee/wuu/ci.yml?branch=main&style=flat-square&label=ci"></a>
    <a href="https://github.com/blueberrycongee/wuu/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/github/license/blueberrycongee/wuu?style=flat-square"></a>
    <a href="https://github.com/blueberrycongee/wuu/releases"><img alt="GitHub release downloads" src="https://img.shields.io/github/downloads/blueberrycongee/wuu/total?style=flat-square&label=downloads"></a>
  </p>
</div>

<img width="2272" height="2494" alt="wuu 桌面应用" src="https://github.com/user-attachments/assets/2d9030aa-ca03-42b1-9333-f79cc5aff95b" />

如果你用过 OpenCode、Claude Code 或 Codex，wuu 的上手方式不会陌生。最简单的理解是：**它把本地 Coding Agent 的工作方式做成了一个桌面 GUI。**

你仍然是打开一个项目，然后和 Agent 一起工作。区别在于，wuu 把这套工作流放进了一个完整的桌面工作区：左侧管理项目和会话，中间处理当前任务，文件、diff、终端、浏览器、技能和模型设置都在旁边。

wuu 是独立开发的项目，不是 OpenCode、Claude Code 或 Codex 的官方客户端。

> [!WARNING]
> wuu 仍处于早期预览阶段，正在快速迭代。打包版本目前支持 Apple 芯片 Mac。

## 下载

从 [GitHub Releases](https://github.com/blueberrycongee/wuu/releases/latest) 下载最新版本，把 `wuu.app` 移到 `/Applications` 后打开。

当前预览版尚未签名和公证。如果 macOS 阻止打开，并且你确认安装包来自官方 Release，可以运行：

```bash
xattr -dr com.apple.quarantine /Applications/wuu.app && open /Applications/wuu.app
```

## 桌面端多了什么

- **项目和会话**：在不同仓库之间切换，跨工作区搜索会话，并恢复或派生之前的工作。
- **文件、改动和终端**：浏览仓库、检查当前 Git diff、预览图片和文档、回看命令输出，不必离开应用。
- **可见的 Agent 过程**：任务运行时可以看到工具调用、后台进程、委派工作和附件。
- **不必折腾繁琐的配置文件**：模型服务、权限、技能、记忆和项目规则都可以直接在桌面端管理。
- **开箱即用的完整 Agent 体验**：计划、持久目标、子 Agent、后台任务和持久会话都已内置，无需自行拼装。

## 开始使用

1. 打开**设置 → 模型服务**，连接 Anthropic 或 OpenAI 兼容提供商。
2. 添加一个本地项目文件夹。
3. 新建对话。

模型配置、权限、附件和会话见[快速开始](docs/zh-cn/getting-started/index.md)。

## CLI

桌面应用自带 core，不需要另行安装 CLI。如果要在脚本、CI 或非交互任务中使用 wuu，可以通过 Go 安装：

```bash
go install github.com/blueberrycongee/wuu/cmd/wuu@latest
wuu init

wuu exec "修复失败的测试并验证结果"
wuu exec --json "审查当前 diff"
wuu exec review --uncommitted
```

结构化输出、附件、会话控制和常用参数见 [`wuu exec` 指南](docs/zh-cn/automation/exec.md)。

## 模型与本地数据

- 模型提供商和凭据由你选择；提示词和相关上下文会发送给该提供商。
- 会话、配置、日志和其他本地状态默认保存在 `~/.wuu`。
- 文件改动和命令在选定的本地工作区内执行，并受当前权限模式控制。

在处理不受信任的仓库或敏感数据前，请阅读[安全模型](docs/zh-cn/reference/security-model.md)。

## 项目

- [文档](docs/zh-cn/index.md)
- [路线图](ROADMAP_zh.md)
- [更新记录](CHANGELOG.md)
- [公开评测](evals/)
- [参与贡献](CONTRIBUTING.md)

遇到问题可以[提交 issue](https://github.com/blueberrycongee/wuu/issues)；安全漏洞请按照 [SECURITY.md](SECURITY.md) 报告。

## 许可证

[MIT](LICENSE)
