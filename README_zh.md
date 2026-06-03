# wuu

GUI 优先的 AI 编程助手，包含 Go 后端和桌面应用。

作者姓 Wu，所以叫 wuu —— 目标是把它打磨到让每个开发者写代码时都忍不住 *wuuuuu!*

## 安装

```bash
# Homebrew
brew install blueberrycongee/tap/wuu

# 脚本安装
curl -fsSL https://raw.githubusercontent.com/blueberrycongee/wuu/main/install.sh | sh

# npm
npx wuu@latest

# 从源码
go install github.com/blueberrycongee/wuu/cmd/wuu@latest
```

## 快速开始

```bash
wuu init                              # 写入 .wuu.json
wuu run "描述一下这个仓库"             # 单次 CLI 任务
wuu app-server --workdir .            # 桌面 GUI 使用的后端
cd desktop && npm install && npm run dev  # 本地启动桌面 GUI
```

交互式使用现在放在桌面 GUI 里。`wuu` 二进制主要提供 app-server 后端和非交互式 CLI 工具。

## 版本管理

- `VERSION` 是版本号唯一来源（SemVer，例如 `0.1.0`）。
- 本地默认构建为 `vX.Y.Z-dev`：

```bash
make install
wuu version --long
```

- 发布流程：

```bash
# 1) 修改 VERSION
# 2) 根据 VERSION 创建发布 tag
make tag-release

# 3) 推送 tag，触发 GitHub Release 工作流
git push origin v$(cat VERSION)
```

推送 `v*` tag 后，会由 GitHub Actions + GoReleaser 自动发布二进制产物。

## 功能

- 桌面 GUI，通过 Go app-server 支持对话、工作区上下文和流式会话
- 单次 CLI 任务，用于非交互式场景
- Agent 工具调用循环 —— 在你的仓库里读、写、编辑、搜索、执行命令
- 支持 OpenAI 兼容 API（OpenAI / OpenRouter / one-api 等）和 Anthropic Messages API
- 内置工具：`run_shell`、`git`、`read_file`、`write_file`、`edit_file`、`list_files`、`grep`、`glob`、`web_search`、`web_fetch`
- 编排与会话工具：`spawn_agent`、`fork_agent`、`send_message`、`followup_task`、`wait_agent`、`close_agent`、`list_agents`、`load_skill`
- 后台进程工具：`start_process`、`list_processes`、`stop_process`、`read_process_output`
- 定时任务工具：`schedule_cron`、`cancel_cron`、`list_cron`
- 工具可用范围：
  - 主 GUI/app-server 会话：可用全部工具
  - 子代理：不可用编排工具（`spawn_agent`、`fork_agent`、`send_message`、`followup_task`、`wait_agent`、`close_agent`、`list_agents`）
- 澄清问题：用户意图不明确时，模型直接在回复正文中以普通文字提出澄清问题——wuu 不再提供独立的 `ask_user` 工具
- Follow-up 控制：`send_message` 可向子代理排队发送简短指令；`followup_task` 可让空闲任务基于已保存历史继续新一轮
- 文件工具沙箱化，限制在当前工作区内
- 会话隔离，支持恢复
- 长对话自动压缩上下文

## 配置

配置文件加载优先级：

1. `.wuu.json`（项目级）
2. `wuu.json`
3. `~/.config/wuu/config.json`（全局）

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

## 许可证

MIT
