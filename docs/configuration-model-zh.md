# Wuu 配置模型

Wuu 把配置分成“用户拥有”和“项目补充”两类。核心原则是：打开一个仓库时，
仓库可以描述自己的工作方式，但不能替用户决定凭据发往哪里、读取哪些工作区外
文件，或扩大本地权限。

## 配置来源与顺序

正常启动先读取用户配置，再按顺序叠加项目来源：

1. `~/.wuu/config.json`，或 `WUU_HOME/config.json`
2. 旧版 `~/.config/wuu/config.json`（仅用于兼容与迁移）
3. 项目 `.wuu.json`，不存在时读取 `wuu.json`
4. 项目 `.wuu/settings.json`
5. 项目 `.wuu/settings.local.json`

对象会递归合并，标量和数组由后面的来源替换。加载成功后返回的可写配置路径始终
是用户配置路径，因此桌面设置和模型切换不会意外改写仓库文件。

## 始终由用户拥有的字段

正常启动会忽略所有项目来源中的以下字段，并在标准错误输出一条说明：

- `default_provider`
- `providers`
- `memory`
- `agent.model_roles`
- `agent.permission_mode`

这些字段分别控制默认提供商、端点与凭据来源、全局记忆发现、后台角色的模型路由，
以及 Wuu 的本地权限边界。字段名按 JSON 的大小写匹配规则处理，所以换成
`Providers`、`Memory` 或 `Permission_Mode` 也不会绕过限制。

其他项目行为仍会正常叠加，例如 `agent.append_system_prompt`。项目配置必须符合完整
配置结构；未知字段会直接报错，避免拼写错误被静默忽略。

## 全局记忆与项目规则

全局记忆本来就位于工作区外，默认保存在用户控制的 Wuu 主目录中。项目不能重定向
它的发现目录，但这不会关闭全局记忆，也不会阻止 Wuu 正常读取和更新它。

需要团队共同遵守的长期规则应写进仓库的 `AGENTS.md` 或项目文档。个人偏好、跨项目
经验和自动整理出的记忆则留在用户目录中。

## 显式信任完整配置

自动化场景可以显式选择完整配置：

- `wuu exec --config <path>`：只读取并信任指定文件。
- `wuu exec --ignore-user-config`：忽略用户配置，读取并信任项目 `.wuu.json`
  （或 `wuu.json`）及两个项目 settings 层。

这两种方式会接受文件中的提供商端点、凭据环境变量名、记忆路径、hooks 和 MCP
服务器，因此只应对自己控制的文件使用。普通桌面和 CLI 启动不会通过空 `HOME`
等隐式条件获得这项信任。

## Ultra 主动多 agent 模式

Ultra 是独立于模型和思考强度的会话开关。它打开主动委派政策，并允许匿名 worker
继续编排自己的子任务；关闭时保留原有的主 agent 和 worker 行为。

```json
{
  "agent": {
    "ultra_mode": true,
    "max_parallel": 5
  }
}
```

| 字段 | 填写方式 | 默认值 | 语义 |
| --- | --- | --- | --- |
| `agent.ultra_mode` | `true` / `false` | `false` | 开启主动多 agent 模式。省略或设为 `false` 时不注入 Ultra 政策，也不解锁默认 worker 的递归编排能力。 |
| `agent.max_parallel` | 非负整数；`0` 等同省略 | `5` | 控制可同时执行的匿名 worker 数量。Ultra 不会提高这个值；超出的异步 spawn 进入 `queued`。 |

`queued` 和 `waiting_children` 状态都不占执行槽。子结果唤醒父 worker 做整合不是新
spawn，因此不经过 spawn 排队闸门；整合开始时，实际运行数可能短暂高于
`max_parallel`。负数配置无效。

### Turn 边界与继承

- 顶层 turn 启动时，core 会把会话当前的 Ultra 值快照为该 turn 的生效值。
- worker 在 spawn 时继承父方的生效值。该值随 worker 一起保存，在其整个生命周期、
  后续复活和继续派生的子树中保持不变。
- 子代理完成后触发的合成 completion turn 使用对应运行中的 turn 快照，不重新读取
  一个可能已变化的会话值。
- turn 运行中切换 Ultra 只更新会话配置和界面状态，不改变当前 turn、已经 spawn 的
  worker 或它们的后代。下一次用户发起的顶层 turn 才读取新值。

这样可以避免一棵正在运行的子树在中途被改变能力。默认配置没有
`agent.ultra_mode` 时，快照始终为 `false`，行为与启用 Ultra 前一致。

### App-server 与 CLI

[`config/model/update`](app-server-protocol.md#ultra-mode-configuration) 的请求可以带可选
字段 `ultra`。省略该字段会保留当前值；`{"ultra": true}` 或
`{"ultra": false}` 可以单独更新模式，也可以与模型更新一起原子写入配置。
`initialize`、`config/read` 和 `config/model/update` 的结果都会回读 `ultra` 与
`max_parallel`。

`wuu exec --ultra` 为当前 exec 运行显式开启 Ultra，不写回配置。配合 `--json` 时，
首个 `session_configured` JSONL 事件会回读实际生效的 `ultra` 和 `max_parallel`。
未传 `--ultra` 时，exec 保留配置中的值。

## 从旧项目配置迁移

如果旧项目把提供商放在 `.wuu.json` 中：

1. 运行 `wuu init` 创建用户配置。
2. 把 `default_provider`、`providers`、`memory`、`agent.model_roles` 和
   `agent.permission_mode` 移到用户配置。
3. 在项目文件中保留真正属于仓库的提示词和其他项目行为。

`WUU_HOME` 可以整体移动用户配置、认证、会话、记忆和日志目录。例如设置
`WUU_HOME=/data/wuu` 后，用户配置路径就是 `/data/wuu/config.json`；即使
`HOME` 环境变量没有设置，这个路径仍然有效。

## Windows 上的 shell

bash 语法在所有平台上都是命令执行的契约。Windows 上 wuu 会解析
Git Bash：优先读取 `WUU_GIT_BASH_PATH` 环境变量；否则依次探测标准
安装位置（`%ProgramFiles%\Git`、`%ProgramFiles(x86)%\Git`、
`%LOCALAPPDATA%\Programs\Git`），再从 PATH 上的 `git.exe` 反推
`bash.exe`。全部失败时命令执行会报错并提示安装 Git for Windows。
TTY 模式的后台进程在 Windows 上不可用，会自动退回管道模式。
