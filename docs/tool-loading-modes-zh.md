# 工具加载模式：flat / wuu_tool_search / native

本文说明渐进式工具加载（deferred tools）的三种模式、按 provider 的判定树、以及国产模型的实际落地配置。工具面分桶机制（direct / deferred / hidden）本身见 `docs/tool-surface-assembly-zh.md`，这里只讲"deferred 桶以什么方式到达模型"。

## 三种模式

- **flat**：无渐进加载。deferred 桶整体搬进 direct 桶（`surfaceForToolLoadingMode`，`internal/tools/toolkit.go`），全量工具 schema 直发，`tool_search` 工具和目录文本都不出现。
- **wuu_tool_search**：wuu 自研软件路径。请求里只带 direct 工具 + `tool_search` 工具；deferred 工具以静态目录文本（`# Deferred Tool Catalog`，会话创建时冻结，上限 48KB）注入 system prompt；模型用 `tool_search`（支持 `select:` 精确加载或关键词检索）加载后，该工具的完整 schema 从下一步请求起持续出现（追加在 stable 前缀之后，`CacheStable=false`）。**对任何 provider 都可用**，不要求端点支持任何协议扩展。
- **native**：provider 原生渐进协议（Anthropic `defer_loading` / `tool_reference`）。从第一轮起就发送全部 deferred 工具的完整 schema 并打 `defer_loading` 标记（tools 数组字节恒定，对 prompt 缓存最友好），加载时只在 tool_result 里追加轻量 `tool_reference`，不重发 schema。**要求端点真的实现了该协议**。

缓存代价对比：native 的 tools 字节从头到尾不变；wuu_tool_search 在每次加载新工具的那一轮会使 tools 数组变长、打穿一次其后的缓存前缀（一次性代价，之后以新基线继续命中）；flat 最大但恒定。

## 配置字段与判定树

模式由**全局** `agent.tool_loading` 控制（`"auto" | "flat" | "native" | "wuu_tool_search"`，默认 `auto`；legacy 别名 `agent.tool_search: true/false` 等价于 `wuu_tool_search`/`flat`）。这是 agent 级单值字段，不是逐 provider 字段；让同一开关在不同 provider 上表现不同的，是 `resolveToolLoadingModeForProvider`（`internal/runtime/session.go`）里按 wire 协议做的能力判定：

```
auto（默认）:
  wireOpenAIResponses + 首方 OpenAI + 模型 >= gpt-5.4  -> native
  wireOpenAIResponses + 首方 OpenAI + 模型较旧          -> wuu_tool_search
  wireAnthropicMessages + api.anthropic.com + 非 haiku  -> native
  wireAnthropicMessages + 兼容端点(国产等)              -> flat（除非 per-model options 显式开启,见下）
  wireOpenAIChat（openai / openai-compatible）           -> flat

native（显式）:
  provider 支持     -> native
  provider 不支持   -> 自动降级 wuu_tool_search
  注意: 对 anthropic wire 的判定是"没显式说不支持就当支持"
       （SupportsNativeToolSearchWhenExplicitlyEnabled 不校验 base_url）,
       给国产 anthropic 兼容端点显式设 native 时 wuu 会直接信任端点实现了
       defer_loading 协议——若端点实际不支持会出错。用
       options.anthropicToolSearch: false 显式关掉可退回 wuu_tool_search。

wuu_tool_search（显式）: 任何 provider 都生效
flat（显式）: 任何 provider 都全量直发
```

判定实现：`providerfactory.SupportsNativeToolDiscovery(ByDefault)`（`internal/providerfactory/factory.go`）、`anthropic.SupportsNativeToolSearchByDefault`（`internal/providers/anthropic/client.go`）。

## 国产模型落地

模型族（kimi / deepseek / qwen / glm / minimax）在 modelprofile 层的 deferred 桶内容**完全相同**——差异只在 wire 判定：

- **kimi/moonshot、GLM、DeepSeek、Qwen**（`type: "openai"` / `"openai-compatible"`，即 wireOpenAIChat）：`auto` 下拿到的是 **flat**，没有任何渐进加载。要开启只有一条路——全局设 `agent.tool_loading: "wuu_tool_search"`。wireOpenAIChat 分支不读 providerOptions，**不存在** per-provider 开原生路径的配置口子。
- **MiniMax anthropic 兼容端点**（`type: "anthropic"`，base_url `api.minimaxi.com/anthropic`）：`auto` 下因非首方 base_url 也是 **flat**；两种开启方式：
  1. 全局 `agent.tool_loading: "wuu_tool_search"`（软件路径，稳妥）；
  2. per-model 显式声明端点支持原生协议：

```json
{
  "providers": {
    "minimax": {
      "type": "anthropic",
      "base_url": "https://api.minimaxi.com/anthropic",
      "model": "MiniMax-M3",
      "models": {
        "MiniMax-M3": {
          "options": { "anthropicToolSearch": true }
        }
      }
    }
  },
  "agent": { "tool_loading": "auto" }
}
```

（`options` 首选 wire 中立的 `native_tool_search` 键（openai responses wire 同样识别，可在兼容 responses 端点上开启、也可在首方 OpenAI 上显式关闭）；anthropic wire 兼容旧别名 `anthropicToolSearch` / `toolSearch` / `tool_search`。**先实测端点真的支持 defer_loading 再开**，判别方法：带 `defer_loading:true` 的请求若被端点拒绝或忽略标记直接全量计费，即不支持。）

全局开关同时作用于主模型和 worker 角色（worker 用自己的 provider/model 重新过一遍同一判定）。

## 相关防线

- `ValidateActiveToolSurfaceForProvider`（会话创建时）对 direct **和** deferred 工具全量做结构校验（名字模式、description ≤ 16KB、schema 字节按 provider 限额 128KB/256KB/512KB），保证渐进加载不会把 schema 违规推迟到 tool_search 触发那一刻才暴露。
- deferred 目录文本是会话创建时冻结的静态段，超 48KB 直接报错（不静默截断）。
- `agent.experimental_deferred_tool_bundles`（默认 false）是正交实验功能：工具成功后连带解锁关联工具，与本文三模式无关。

## 设计边界

wire 层的默认判定以首方 base_url / 模型版本号为启发（auto 模式的合理缺省），anthropic 与 openai responses 两个 wire 都接受统一的 per-model `options.native_tool_search` 显式覆盖——配置永远赢过 URL 启发。openai chat wire 没有原生渐进协议可开，覆盖键对它无意义，软件路径 `wuu_tool_search` 是唯一选项。
