# 用户指南

这份指南覆盖从安装 wuu 到完成日常工作的稳定路径。每个任务都在一个本地工作区中
执行，并使用你自己配置的凭据调用模型提供商。

## 选择使用方式

- **桌面端：**适合交互式工作、多会话、附件和可视化审查。
- **CLI：**通过 `wuu exec` 从终端、脚本、CI 或其他 agent 调用。

桌面应用自带私有 core，不依赖单独安装的 CLI。两者可以同时存在，版本也可能不同。

## 安装

### macOS 桌面预览版

从 [GitHub Releases](https://github.com/blueberrycongee/wuu/releases) 下载 arm64
DMG 或 ZIP，然后把 `wuu.app` 移到 `/Applications`。

当前预览版没有签名和公证。如果确认下载来源可信，但 macOS 阻止打开，可以移除
quarantine 标记：

```bash
xattr -dr com.apple.quarantine /Applications/wuu.app
open /Applications/wuu.app
```

不要对来源不可信的应用运行这条命令。

### CLI

使用 Go 安装 CLI：

```bash
go install github.com/blueberrycongee/wuu/cmd/wuu@latest
wuu --version
```

GitHub Releases 不提供单独的 CLI 压缩包。

## 连接模型提供商

wuu 使用 BYOK 模式：模型请求使用你自己配置的提供商和凭据。

### 桌面端

打开**设置 → 提供商**，选择或添加提供商，填写模型、端点和 API Key，然后保存，
再开始会话。

### CLI

首次使用时创建用户配置：

```bash
wuu init
```

默认写入 `~/.wuu/config.json`；设置了 `WUU_HOME` 时写入
`$WUU_HOME/config.json`。初始配置包含 OpenAI、Anthropic 和 OpenRouter。按照所选
提供商的 `api_key_env` 设置环境变量，例如：

```bash
export OPENAI_API_KEY="..."
wuu exec "描述一下这个仓库"
```

单次运行切换到另一个已配置的提供商：

```bash
wuu exec --provider anthropic "审查当前改动"
```

配置覆盖顺序和信任边界见[配置模型](../reference/configuration.md)，完整自动化参数见
[`wuu exec`](../../en/automation/exec.md)（英文）。

## 在正确的仓库中工作

工作区决定文件工具、命令和可见会话的范围。

- 桌面端先打开或选择本地项目文件夹，再开始会话。
- CLI 在目标仓库中运行，或者显式传入 `--workdir`。

```bash
cd path/to/project
wuu exec "运行测试并修复失败"

wuu exec --workdir path/to/project "总结这个代码库"
```

描述你真正想得到的结果；如果有影响产品行为、安全、兼容性或数据的约束，也一并
说明。常规实现细节可以让 wuu 先检查仓库后自行判断。

## 添加文件和图片

桌面输入框支持附件。CLI 需要显式传入：

```bash
wuu exec --file report.pdf "总结这份报告"
wuu exec --image screenshot.png "找出这个界面的问题"
```

附件只让当前任务能够读取该文件，不会自动把它加入仓库。

## 继续和查看会话

wuu 会保存持久会话，方便跨多次运行继续工作：

```bash
wuu exec --continue "继续上一个会话"
wuu exec --resume THREAD_ID "继续这个任务"
wuu session list
wuu session show --last
```

自动化任务不需要保存会话时使用 `--ephemeral`。`wuu session archive` 只会把会话
从普通列表中隐藏；只有确定要移除已保存历史时才使用 `wuu session delete`。

## 理解信任边界

- wuu 操作本地文件，并按当前权限模式运行本地命令。
- 提示词和相关上下文会发送给配置的模型提供商。BYOK 不代表模型在本地运行。
- API Key 应通过桌面提供商设置或环境变量提供，不要提交到仓库。
- 正常启动时，提供商端点、凭据、记忆路径和权限模式始终由用户控制；项目配置不能
  静默替换这些内容。
- 用户配置、会话、日志和其他状态默认位于 `~/.wuu`。设置 `WUU_HOME` 可以整体迁移
  这些状态。

处理不可信仓库或敏感数据前，请先阅读[安全模型](../../en/reference/security-model.md)（英文）。

## 常见问题

### `wuu: command not found`

确认 Go 的二进制目录已经加入 `PATH`：

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

如果这样可以解决问题，再把等价配置加入 shell 启动文件。

### 提供商提示缺少 API Key

确认配置中的 `api_key_env` 与实际导出的环境变量名一致，并从能读取该变量的环境中
启动 wuu。桌面端应在**设置 → 提供商**中填写 Key，不要依赖只对某个终端生效的
`export`。

### 出现了错误的仓库或会话

检查桌面端当前选择的工作区、终端当前目录或 `--workdir`。默认情况下，会话列表按
工作区隔离。

### `wuu init` 提示配置已存在

直接编辑已有配置，不要覆盖。`wuu init --force` 会替换文件，只有在备份好需要保留的
内容后才应使用。

### macOS 不允许打开桌面应用

先确认应用来自官方 GitHub Releases，再运行安装章节中的 quarantine 命令。当前预览版
确实没有签名和公证。
