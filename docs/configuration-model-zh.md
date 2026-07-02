# Wuu 配置模型与迁移设计

这份文档定义 Wuu 的双入口配置策略：配置文件给高级用户和 agent 精准编辑，桌面 Settings 给迁移用户快速完成常用配置。目标用户包括从 Claude Code、PI context、OpenCode 迁移过来的用户。

## 设计原则

配置文件是完整来源。Wuu 读取严格 JSON，未知字段会报错；agent 应该直接编辑 `.wuu.json` 或 `~/.config/wuu/config.json`，不要写 Wuu 不认识的 OpenCode/Claude Code 原字段。

桌面 Settings 是高频入口。UI 只暴露安全、常用、能即时解释的字段；复杂模型目录、权限细则、hook、MCP 连接细节、实验开关由用户或 agent 手动添加。

默认值尽量省略。UI 保存时会删除等于默认值或自动值的字段，让配置文件保持短小，便于迁移用户理解，也便于 agent 做精确 diff。

## 配置文件位置

Wuu 的读取顺序是：

1. 工作区 `.wuu.json`
2. 工作区 `wuu.json`
3. 用户全局 `~/.config/wuu/config.json`

`wuu exec --config <path>` 可以指定单次运行使用的配置文件；`--env KEY=VALUE`、`--allow-tool`、`--deny-tool` 是单次运行覆盖，不写回配置。

`wuu init` 生成完整 starter config，默认 `default_provider` 是 `openai`。桌面 app-server 在没有配置时会创建更窄的全局 starter config，默认使用 `openai-codex`，方便从 Codex / ChatGPT Codex 迁移的用户先跑起来。

## UI 分层

| 暴露策略 | 适用字段 | 用户体验 |
| --- | --- | --- |
| 默认可见 | Provider 切换/新增、模型、Base URL、API key/Auth token、思考强度、附加系统提示、记忆开关、MCP 开关 | Settings 打开即可改 |
| 高级页 | 自动压缩、上下文窗口、最大步数、temperature | Settings 的高级页可改 |
| 文件手动 | model catalog、权限细则、model roles、MCP command/env/oauth/tool metadata、hooks、memory discovery、tool loading、provider transport | 用户或 agent 手动加 JSON |
| 开发隐藏 | 调试入口、工具 surface 调试视图 | 只在开发构建出现，默认关闭 |

## 迁移映射

| 来源 | Wuu 入口 | 迁移方式 | UI 策略 |
| --- | --- | --- | --- |
| Claude Code 的持久指令 | `AGENTS.md`、`AGENTS.override.md`、`CLAUDE.md`、`memory.include_legacy_memory` | Wuu 默认读 Wuu 原生指令文件；需要显式迁移 Claude 风格旧记忆时，把 `memory.include_legacy_memory` 设为 `true` | 记忆开关默认可见；legacy import 文件手动 |
| Claude Code / Codex 的模型服务 | `providers.*`、`default_provider`、`agent.effort` / `agent.variant` | Anthropic 走 `type: "anthropic"`；OpenAI 兼容网关走 `type: "openai-compatible"`；Codex subscription 走 `type: "openai-codex"` | 常用 Provider 默认可见；Codex OAuth 由专门连接流管理 |
| Claude Code / OpenCode 的 MCP | `mcp_servers` | Wuu 字段名是 `mcp_servers`，不是 OpenCode 的 `mcp`；本地服务填 `command` + `args`，远程服务填 `url` | 已存在 server 的启停默认可见；新增/细节文件手动 |
| OpenCode 的 provider/model catalog | `providers.*.models.*` | Wuu 接受 OpenCode/models.dev 风格的模型 metadata，用来派生变体、能力和 token limit | 文件手动 |
| OpenCode 的 permission | `agent.permission_mode`、`agent.permission_rules`、`agent.tool_policy` | 先选粗粒度 `permission_mode`，再用 `permission_rules` 或 `tool_policy` 精修 | 粗粒度可进入 Provider/安全 UI；细粒度文件手动 |
| PI context 的上下文管理 | `memory.*`、`agent.compact_*`、`agent.disable_auto_compact` | PI 更像上下文/记忆工作流参考；Wuu 对应长期记忆、自动压缩、保留最近上下文 | 记忆开关和压缩参数可见；发现规则文件手动 |

## 最小配置示例

```json
{
  "default_provider": "openrouter",
  "providers": {
    "openrouter": {
      "type": "openai-compatible",
      "base_url": "https://openrouter.ai/api/v1",
      "api_key_env": "OPENROUTER_API_KEY",
      "model": "openai/gpt-4.1-mini"
    }
  },
  "agent": {
    "permission_mode": "agent",
    "append_system_prompt": "默认用中文回答。"
  }
}
```

密钥优先用 `api_key_env` / `auth_token_env` 指向环境变量。UI 可以写入 `api_key` / `auth_token`，但高级用户和 agent 应优先使用环境变量，避免把密钥直接放进项目配置。

## 字段设计

### 顶层字段

| 字段 | 填写方式 | 默认值 | UI 策略 |
| --- | --- | --- | --- |
| `default_provider` | `providers` 里的 key，例如 `openrouter` | `wuu init` 为 `openai`；桌面 starter 为 `openai-codex` | Provider 页默认可见 |
| `providers` | provider 名到连接配置的对象 | starter config 自带常用 provider | Provider 页默认可见，复杂字段手动 |
| `agent` | Agent 运行时配置对象 | 可省略；省略后套用 Agent 默认值 | 部分默认可见，部分高级页，复杂字段手动 |
| `memory` | 记忆发现、长期记忆和后台整理配置 | 可省略；记忆默认开启 | 记忆开关默认可见，其余手动 |
| `mcp_servers` | MCP server 名到连接配置的对象 | 可省略；没有 MCP | 已配置 server 的开关和状态可见，新增/细节手动 |
| `hooks` | hook 事件名到 hook 列表的对象 | 可省略；没有 hook | 文件手动 |

### Provider 字段：`providers.<name>`

| 字段 | 填写方式 | 默认值 | UI 策略 |
| --- | --- | --- | --- |
| `type` | `openai`、`openai-compatible`、`codex`、`openai-codex`、`codex-subscription`、`chatgpt-codex`、`anthropic` | 必填；starter 已填 | 新增 Provider 时默认可见；OAuth 型手动或专用连接流 |
| `type` 别名 | `claude`、`anthropic-official`、`bedrock`、`gemini` | 读取时由 `internal/modelcatalog/catalog.go` 映射到对应规范名（`claude`、`anthropic-official` → `anthropic`；`bedrock` → `amazon-bedrock`；`gemini` → `google`），`providerfactory/factory.go` 不视作第一档 `type` | 文件手动；常见于现存配置 |
| `base_url` | API 根地址，例如 `https://api.openai.com/v1` | 普通 provider 必填；Codex subscription 可省略但 starter 会写 | 默认可见 |
| `api` | OpenCode/model metadata 的 API 标识 | 空 | 文件手动 |
| `npm` | OpenCode/model metadata 的包名 | 空 | 文件手动 |
| `wire_api` | `chat` 或 `responses` | OpenAI 兼容默认 `chat`；Codex subscription 必须 `responses` | 文件手动 |
| `api_key` | 明文 API key | 空 | UI 可写；不建议项目配置手动写 |
| `api_key_env` | 环境变量名，例如 `OPENROUTER_API_KEY` | 普通 API-key provider 为空时，OpenAI/Codex 系会看 `OPENAI_API_KEY`，Anthropic 系会看 `ANTHROPIC_API_KEY` | 文件手动，推荐 |
| `auth_token` | Bearer token，Anthropic 兼容场景可用 | 空 | UI 可写 |
| `auth_token_env` | Bearer token 的环境变量名 | 空时会看 `ANTHROPIC_AUTH_TOKEN` | 文件手动，推荐 |
| `model` | 发送给上游的模型名 | 必填；starter 已填 | 默认可见 |
| `models` | 模型名到 model metadata 的对象 | 空 | 文件手动 |
| `headers` | 额外 HTTP header map | 空 | 文件手动 |
| `reuse_codex_credentials` | `true` / `false` | `false`；`openai-codex` starter 为 `true` | 文件手动或 Codex 连接流 |
| `stream_connect_timeout_ms` | 正整数毫秒 | `0` 表示 provider 默认 | 文件手动 |
| `stream_idle_timeout_ms` | 正整数毫秒 | `0` 表示 provider 默认 | 文件手动 |
| `stream_transport` | `auto`、`sse`、`websocket`、`websocket-cached` | `auto` | 文件手动 |
| `context_window` | token 数，适合未知模型或网关别名 | `0` 表示使用内置模型目录 | 高级页可见 |
| `cache_creation_input_tokens_omitted` | `true` / `false` | `false` | 文件手动 |

### Provider model 字段：`providers.<name>.models.<model>`

这些字段主要服务 OpenCode/models.dev 迁移和私有模型目录，不进入第一屏 UI。

| 字段 | 填写方式 | 默认值 | UI 策略 |
| --- | --- | --- | --- |
| `id`、`name`、`family`、`status`、`release_date` | 字符串 metadata | 空 | 文件手动 |
| `reasoning`、`attachment`、`tool_call`、`structured_output`、`temperature` | 布尔能力标记 | 空，表示未知 | 文件手动 |
| `reasoning_options` | 对象数组，保留上游 metadata | 空 | 文件手动 |
| `interleaved` | 任意 JSON metadata | 空 | 文件手动 |
| `modalities.input`、`modalities.output` | 字符串数组，例如 `["text", "image"]` | 空 | 文件手动 |
| `cost` | 价格 metadata 对象 | 空 | 文件手动 |
| `provider.api`、`provider.npm` | 单模型 provider override | 空 | 文件手动 |
| `limit.context`、`limit.input`、`limit.output` | token limit | 空 | 文件手动 |
| `options` | provider 选项对象 | 空 | 文件手动 |
| `headers` | 单模型 header map | 空 | 文件手动 |
| `supported_efforts` | 例如 `["low", "medium", "high"]` | 空 | 文件手动；UI 可据此渲染思考强度 |
| `default_effort` | effort 字符串 | 空 | 文件手动 |
| `default_variant` | variant key | 空 | 文件手动 |
| `variants` | variant 名到 provider options 的对象 | 空 | 文件手动；UI 可据此渲染思考强度 |
| `disabled` | `true` / `false` | `false` | 文件手动 |
| `context_window` | token 数 | `0` 表示不用单模型覆盖 | 文件手动 |

### Agent 字段：`agent`

| 字段 | 填写方式 | 默认值 | UI 策略 |
| --- | --- | --- | --- |
| `name` | profile 名；默认 profile 写 `default` 或省略 | `default` | 文件手动 |
| `max_steps` | 非负整数，`0` 表示不限 | `0` | 高级页可见 |
| `max_context_tokens` | 非负整数，`0` 表示自动 | `0` | 高级页可见 |
| `temperature` | 大于 `0` 到 `2`；省略或 `0` 表示 Auto，会省略请求参数 | Auto | 高级页可见 |
| `compact_threshold_pct` | `0` 到 `<1` 的小数，例如 `0.5` | `0` 表示自动 | 高级页可见，UI 用百分比填写 |
| `compact_keep_recent_tokens` | 非负整数 | `0` 表示默认 `20000` | 高级页可见 |
| `system_prompt` | 旧字段，追加到内置提示后 | 空 | 文件手动；新配置不要新增 |
| `append_system_prompt` | 用户/项目补充指令 | 空 | 常规页默认可见 |
| `permission_mode` | `read_only`、`agent`、`auto_review`、`full_access` | `agent` | 应默认可见或随 Provider 保存；目前协议已支持 |
| `permission_profile` | `read_only`、`workspace_write`、`danger_full_access` | 由 `permission_mode` 推导，`agent` 对应 `workspace_write` | 文件手动 |
| `approval_policy` | `on_request`、`never` | 由 `permission_mode` 推导，`agent` 对应 `on_request` | 文件手动 |
| `approvals_reviewer` | `user`、`auto_review` | 由 `permission_mode` 推导，`agent` 对应 `user` | 文件手动 |
| `permission_rules` | OpenCode 风格 `permission -> pattern -> allow/deny/ask` | 空 | 文件手动 |
| `tool_policy.default_action` | `allow`、`deny`、`require_approval`、`auto_classify` | 空，由权限模式决定 | 文件手动 |
| `tool_policy.tools` | tool 名到 action | 空 | 文件手动 |
| `tool_policy.kinds` | tool kind 到 action | 空 | 文件手动 |
| `tool_policy.risks` | `low`、`medium`、`high` 到 action | 空 | 文件手动 |
| `effort` | `low`、`medium`、`high`、`max`，空表示 API 默认 | 空 | Provider 页默认可见为“思考强度” |
| `variant` | provider/model 变体名；有 variants 时优先于 `effort` | 空 | Provider 页默认可见为“思考强度” |
| `model_roles` | role 到 provider/model/effort/variant | 空，继承主模型 | 文件手动 |
| `disable_auto_compact` | `true` / `false` | `false` | 高级页可见，UI 反向显示为“自动压缩” |
| `catwalk_autoupdate` | `true` / `false` | `false` | 文件手动 |
| `tool_loading` | `auto`、`flat`、`native`、`wuu_tool_search` | `auto` | 文件手动 |
| `tool_search` | `true` / `false` | `null` | 文件手动 |
| `experimental_deferred_tool_bundles` | `true` / `false` | `false` | 文件手动 |
| `experimental_coordinator_mode` | `true` / `false` | `false` | 文件手动 |

### Model role 字段：`agent.model_roles.<role>`

支持的 role 是 `review`、`compact`、`title`、`memory`、`worker`、`fallback`。

| 字段 | 填写方式 | 默认值 | UI 策略 |
| --- | --- | --- | --- |
| `provider` | `providers` 里的 key | 空，继承主 provider | 文件手动 |
| `model` | 模型名 | 空，继承该 provider 的 model | 文件手动 |
| `effort` | effort 字符串 | 空，继承主 effort | 文件手动 |
| `variant` | variant 字符串 | 空，继承主 variant | 文件手动 |

### Memory 字段：`memory`

| 字段 | 填写方式 | 默认值 | UI 策略 |
| --- | --- | --- | --- |
| `filenames` | 文件名数组，按优先级扫描 | `["AGENTS.md", "AGENTS.override.md", "CLAUDE.md"]` | 文件手动 |
| `project_root_markers` | 根目录标记数组 | `[".git", ".hg", ".jj", ".svn"]` | 文件手动 |
| `user_dirs` | 用户级记忆目录数组，支持 `~` | `["~/.config/wuu"]` | 文件手动 |
| `include_legacy_memory` | `true` / `false` | `false` | 文件手动 |
| `disable` | `true` / `false` | `false` | 常规页默认可见为“记忆”开关 |
| `nudge_interval` | 成功用户轮数；`0` 关闭后台 reviewer | `10` | 文件手动 |
| `memory_char_limit` | workspace memory 字符上限 | `2200` | 文件手动 |
| `user_char_limit` | user memory 字符上限 | `1375` | 文件手动 |
| `dream_interval_days` | 天数；`0` 每次新会话都可触发，负数非法 | `7` | 文件手动 |

### MCP 字段：`mcp_servers.<name>`

| 字段 | 填写方式 | 默认值 | UI 策略 |
| --- | --- | --- | --- |
| `command` | 本地 MCP server 可执行命令 | 空；本地 server 需要 | 文件手动 |
| `args` | 命令参数数组 | 空 | 文件手动 |
| `url` | 远程 MCP server URL | 空；远程 server 需要 | 文件手动 |
| `env` | 本地 server 环境变量 map | 空 | 文件手动 |
| `headers` | 远程 server header map | 空 | 文件手动 |
| `oauth.client_id` | OAuth client id | 空 | 文件手动 |
| `oauth.client_secret` | OAuth client secret | 空 | 文件手动 |
| `oauth.scopes` | scope 字符串数组 | 空 | 文件手动 |
| `oauth.redirect_uri` | redirect URI | 空 | 文件手动 |
| `enabled` | `true` / `false`；省略等于 `true` | `true` | 常规页默认可见，仅对已有 server 启停 |
| `tool_overrides.<tool>.read_only` | 布尔，修正 MCP tool 元数据 | 空，使用 server 声明 | 文件手动 |
| `tool_overrides.<tool>.concurrency_safe` | 布尔，修正并发安全元数据 | 空，使用 server 声明 | 文件手动 |
| `tool_overrides.<tool>.capability` | capability 字符串，例如 `search.semantic`、`file.edit` | 空，使用 server 声明 | 文件手动 |

### Hook 字段：`hooks.<event>[]`

支持的 event 是 `PreToolUse`、`PostToolUse`、`PostToolUseFailure`、`UserPromptSubmit`、`SessionStart`、`SessionEnd`、`Stop`、`FileChanged`。

| 字段 | 填写方式 | 默认值 | UI 策略 |
| --- | --- | --- | --- |
| `matcher` | tool 名 pattern，`*` 或空表示匹配全部 | 空 | 文件手动 |
| `type` | `command` 或 `prompt` | `command` | 文件手动 |
| `command` | shell command，`type: "command"` 时使用 | 空 | 文件手动 |
| `prompt` | prompt hook 的评估提示 | 空 | 文件手动 |
| `model` | prompt hook 使用的模型 | 空，使用默认模型 | 文件手动 |
| `timeout` | 秒数 | `30` | 文件手动 |

## UI 收尾清单

当前 Settings 已经覆盖第一阶段的迁移闭环：

- Provider 页：选择、新增、删除 provider，编辑模型、Base URL、API key/Auth token、思考强度。
- 常规页：编辑 `agent.append_system_prompt`、切换 `memory.disable`、切换已有 `mcp_servers.*.enabled`，查看 MCP 状态。
- 高级页：编辑 `agent.max_steps`、`agent.max_context_tokens`、`agent.temperature`、`agent.compact_threshold_pct`、`agent.compact_keep_recent_tokens`、`agent.disable_auto_compact`、`providers.<active>.context_window`。
- 开发页：调试入口只在开发构建出现，并且默认关闭。

下一阶段可以考虑把 `agent.permission_mode` 做成默认可见的权限预设控件；细粒度 `permission_rules` 和 `tool_policy` 仍保持文件手动，避免 UI 误导用户以为它们是简单开关。
