# 工具面拼装链路

本文描述一个工具从代码定义到进入 provider API 请求的完整路径，以及切换 provider / 模型 / agent 角色时工具面如何变化。适合需要新增工具、调整门控、或排查"模型看得到指引却调不到工具"一类问题的开发者。

## 链路总览

```
工具定义 (internal/tools/tool_*.go)
   │  实现 providers.Tool 接口
   ▼
registry (toolkit.rebuildRegistry)
   │  全量注册，与角色/模型无关
   ▼
capability Surface (internal/capability)
   │  按能力词表切出 Tools / DeferredTools / HiddenTools 三桶
   ▼
modelprofile 编译 (internal/modelprofile/compiler.go)
   │  按 ProfileKey(模型族) x SurfaceKind(角色) 决定桶里放什么
   ▼
toolkit 投影 (Toolkit.SetActiveProfile / Definitions)
   │  Surface 当白名单，产出发往 provider 的工具定义序列
   ▼
prompt builder (internal/prompt + runtime/session.go)
   │  工具相关的系统提示段（tool_surface / tool_discovery / deferred catalog）
   ▼
provider client (internal/providers/{anthropic,openai,...})
      按 wire 协议序列化 schema，进请求
```

## 各站职责

### 工具定义与注册

每个工具是 `internal/tools/tool_*.go` 里一个实现 `providers.Tool` 的结构体。所有工具构造时共享 `tools.Env`（`internal/tools/env.go`）作为运行时状态容器。注册发生在 `toolkit.rebuildRegistry`（`internal/tools/toolkit.go`），这一层不做任何裁剪。

### capability 门控

能力词表在 `internal/capability/capability.go`（如 `CapabilityGoal`、`CapabilitySchedule`、`CapabilityWorkflow`）。`Surface`（`internal/capability/surface.go`）把工具分三桶：

- `Tools`：直接可见，schema 随请求发送；
- `DeferredTools`：不随请求发送，模型通过 `tool_search` 按需加载；
- `HiddenTools`：完全不可见。

`Surface.HasAvailableCapability()` 判断"直接可见或 deferred 可搜到（排除 hidden-only）"，是系统提示里能力指引的门控依据——保证不会出现"教了却调不到"。

### modelprofile 编译

`DefaultCompiler.Compile(p, kind)`（`internal/modelprofile/compiler.go`）按两个维度决定工具面：

- **ProfileKey（模型族）**：`compileOpenAICodex / compileGPT / compileAnthropicClaude / compileGeneric` 四个编译器。差异主要在编辑原语（Codex/GPT 走 `apply_patch`，Claude/Generic 走 `edit_file` + `write_file`）、`AllowDirectShell=false` 的本地模型不出 `bash`、以及 per-profile 的 `SystemFragment`。
- **SurfaceKind（角色）**：
  - `SurfaceWorker`（子 agent）：只加 `agent_report`，无 task 编排套件、无 helpme、无 workflow；
  - `SurfaceMain`（普通 project agent）：task 套件（`spawn_agent` 可见，`send_message` / `close_agent` deferred）+ helpme，不含 workflow；
  - `SurfaceNamed`（named / 常驻 agent）：在 Main 之上加 workflow 套件（单一真源 `NamedWorkflowTools()`）、`fetch_thread_messages`、participant speech 工具。运行时把普通 surface 升级为 named 的 patch 路径（`enableResidentParticipantSurface`）消费同一个 `NamedWorkflowTools()`，与编译期不漂移。

### toolkit 投影

`Toolkit.Definitions()`（`internal/tools/toolkit.go`）以 Surface 为白名单产出最终定义序列，分四段：direct（`CacheStable=true`，进稳定前缀）、deferred（`CacheStable=false`）、MCP（动态）、subagent 管理（尾部）。

### 系统提示中的工具指引

`buildBaseSystemPromptResult`（`internal/runtime/session.go`）按固定顺序插入静态段：`base` → `workflow_path`（门控见下）→ `harness_adapter` → `tool_surface` → `tool_discovery` + `deferred_tool_catalog`（有 `tool_search` 时）→ `subagent_types`（有 `spawn_agent` 时）→ `environment` → `user_custom_prompt`，之后才是动态段（memory / memdir / skills / workflows）。

workflow 指引自 2026-07 起不再内嵌于 `prompts/system_main.md`，改为 `AddWorkflowPathGuidance()` 注入，门控条件是 `toolSurface.HasAvailableCapability(CapabilityWorkflow)`；workflow 目录（catalog）额外要求"无 tool_search 才注入"（有 tool_search 时靠 deferred 自取）。因此普通 project agent 既看不到 workflow 指引也没有 workflow 工具，门控自洽。

## provider 差异

### wire 协议与 schema 序列化

- anthropic wire：`{name, description, input_schema}`（`buildAnthropicTools`，`internal/providers/anthropic/client.go`）；
- openai wire：`{type: "function", function: {name, description, parameters}}`（`internal/providers/openai/client.go`）。

同一份 JSON Schema 只做包装形状转换，不改内容。唯一的内容级改写是 `providers.ToolInputSchemaForModel`（`internal/providers/tool_schema.go`）：仅 kimi/moonshot 与 gemini 两族模型有 sanitize，GPT / Claude / 其他原样透传。

### 工具面随什么变、不随什么变

随 **模型族**（编辑原语、shell 可用性）和 **角色**（SurfaceKind）变；不随 base_url / 具体厂商变——同一 profile 下所有 anthropic 兼容端点收到的工具字节一致。这是刻意的：工具 schema 位于 prompt 缓存前缀内（Anthropic 缓存顺序 tools → system → messages），任何字节抖动都会打穿整个前缀缓存。

## 缓存红线：schema 必须字节稳定

工具 schema 内禁止任何会话内可变内容（时间戳、cwd、动态 roster、随机 id）。有动态需求时的出路：

- 会话内稳定、跨会话可变的内容（如 subagent 类型列表）放静态系统段（`subagentTypesSystemSection`），不进 schema；
- 每轮变化的内容（如 `<subagent_status>` reminder）走 `RequestOnlyContextBlocks`，注入在消息尾部、sliding tail 断点之后，永不进 durable history，也不碰前缀。

参照系：Claude Code 对工具 base schema 做 session 级缓存冻结（首次计算后定格字节），pi 用静态 TypeBox schema + per-provider 包装函数，两者与 wuu 的约束一致。codex 的工具 schema 会随 feature 开关每轮重建，但它走 OpenAI Responses 的服务端自动前缀缓存，约束形态不同，不可直接类比。

## 2026-07 工具面瘦身之后的形态

`561083c4` 把工具面从约 35 个砍到 20 个：

- 删除死代码：`run_shell`、`start/stop/list/read/write_process`、`run_test`（bash 六 action 已覆盖）；
- 编排精简：`await_agents` 删除（冲突检测挪到子 agent 完成唤醒路径）、`followup_task` 并入 `send_message`（`trigger_turn` 参数）、`list_agents` 降级为 `<subagent_status>` reminder；
- CRUD 合并为 action 枚举单工具：`goal(action: get|create|update)`、`cron(action: list|add|remove)`；
- `decline` 并入 `post_message(kind)`；`create_group` / `add_group_member` 并入 `manage_participant(action)`，execute-time fail-closed 门禁。

合并出的 5 个工具 schema 全部为静态扁平枚举，无动态内容。历史会话渲染兼容：`desktop/src/renderer/ToolActivityHelpers.ts` 保留旧工具名到中文 label 的映射，只用于渲染旧记录，不驱动模型。

设计取舍备注：codex 对同类 goal 功能保留三个独立工具（`ext/goal`），未做 action 合并。合并的收益是更小的稳定前缀和更少的工具数（对国产模型的工具调用可靠性友好），代价是单工具 description 更长、参数校验从 schema 层挪到 execute 层。两种做法都成立，wuu 选择合并主要服务于 BYOK 场景下的缓存稳定与可移植性。
