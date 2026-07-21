# wuu 路线图

[English](ROADMAP.md)

wuu 正在成为一个可靠、开放、BYOK 的编码 Agent：既能通过桌面应用与人协作，也能
通过稳定的核心被脚本、CI、其他 Agent 和未来的客户端调用。

这份路线图描述的是**结果**，不是固定的发版日历。我们会根据真实使用情况调整优先级。
GitHub issue 保存具体方案和任务；[更新日志](CHANGELOG.md)记录真正已经发布的内容。

## 怎么看这份路线图

- **Now（现在）**——当前重点。在继续扩张能力之前，这些结果需要先变得可靠。
- **Next（接下来）**——当前基础稳定后要建设的下一层产品能力。
- **Later（以后）**——值得探索、但还没有排期的方向。

一个想法出现在这里，不代表已经承诺日期或最终设计，只代表产品方向。

## 产品原则

- **完成真实工作。** 优先交付工作区中经过验证的成果，而不是看起来很厉害的对话。
- **让 Agent 的工作可检查。** 用户应该知道运行了什么、改了什么、消耗了多少，以及
  哪里还需要处理。
- **能够恢复，而不是从头再来。** 长任务、后台工作、中断和应用重启都应有明确、持久的状态。
- **融入现有环境。** 在安全的前提下复用项目指令、Skills、模型和自动化，不强迫用户
  先建立一套 wuu 专用配置。
- **坚持一个可复用核心。** 桌面、CLI 自动化和未来客户端通过 Go core 与
  `app-server` 共享行为。
- **先赢得信任，再增加权限。** 新集成和原生能力需要明确来源、权限、生命周期和失败状态。

## Now——让日常编码工作可靠

### 可靠的执行与恢复

把长时间命令、子 Agent、中断、排队输入、取消和恢复收敛成一套容易理解的生命周期。
任务不应静默消失、不应在所属会话结束后失控运行，也不应让用户猜测它是否仍在进行。

相关工作：[#157](https://github.com/blueberrycongee/wuu/issues/157)、
[#156](https://github.com/blueberrycongee/wuu/issues/156) 和
[#31](https://github.com/blueberrycongee/wuu/issues/31)。

### 可审查、可追溯的变更

让文件修改、命令和 Agent 产生的变更集易于检查，也能方便地交给独立会话复查。
减少模型看到的工具输出时，仍然保留用户需要的持久审计记录。

相关工作：[#151](https://github.com/blueberrycongee/wuu/issues/151) 和
[#103](https://github.com/blueberrycongee/wuu/issues/103)。

### 补齐桌面端的日常闭环

降低添加文件、跟踪命令、切换 Git 上下文、管理定时任务以及查看仓库、PR 和 CI 状态的
摩擦。这些能力应该像同一个工作区的组成部分，而不是互不相干的面板。

相关工作：[#130](https://github.com/blueberrycongee/wuu/issues/130)、
[#135](https://github.com/blueberrycongee/wuu/issues/135)、
[#57](https://github.com/blueberrycongee/wuu/issues/57) 和
[#56](https://github.com/blueberrycongee/wuu/issues/56)。

### 让 BYOK 模型服务更容易理解

在不保存私密提示内容的前提下，让模型可用状态和请求消耗更清楚。兼容模型目录应该能
及时更新，不必为了每个新模型都发布一个 wuu 版本。

相关工作：[#148](https://github.com/blueberrycongee/wuu/issues/148) 和
[#119](https://github.com/blueberrycongee/wuu/issues/119)。

## Next——融入用户现有的开发环境

### 安全迁移与扩展兼容

打开已有 Codex 和 Claude Code 项目时，立即使用其中安全的文本资产；外部连接和可执行
扩展则必须获得明确授权。绝不静默迁移密钥，也不反向修改其他工具的源配置。

相关工作：[#153](https://github.com/blueberrycongee/wuu/issues/153)。

### 面向生成产物的工作区

让对话能在一等工作区中持续创建和修改成果：先覆盖代码与 Markdown，再扩展到交互式
网页预览以及 DOCX、PPTX 等文档。文件始终是真实来源，离开 wuu 后仍然可用。

相关工作：[#154](https://github.com/blueberrycongee/wuu/issues/154) 和
[#20](https://github.com/blueberrycongee/wuu/issues/20)。

### 让更多客户端复用稳定核心

强化 `app-server` 作为桌面、自动化和未来编辑器集成的统一契约。新的 Shell 应复用会话、
工具、权限和模型行为，而不是重新实现一套 Agent runtime。

## Later——从编码 Agent 走向协作工作空间

这些方向值得探索，但目前没有排期承诺：

- 供人和 Agent 共同使用、支持持久链接与视图的代码库知识空间
  ([#36](https://github.com/blueberrycongee/wuu/issues/36))。
- 用任务图和具名 Agent 取代“聊天室式”多 Agent 编排
  ([#138](https://github.com/blueberrycongee/wuu/issues/138))。
- 支持受控凭据复用和 Agent 原生交互的深度浏览器工作面
  ([#96](https://github.com/blueberrycongee/wuu/issues/96))。
- 在同一核心之上增加更多 Shell，并扩大桌面安装包的平台覆盖，而不是分别维护多套实现。

## 什么叫 1.0

1.0 **不要求**完成所有 Later 项。它代表受支持的核心工作流已经足够可靠，用户可以放心
围绕它建立使用习惯和外部集成。在称为 1.0 之前，我们期望：

- 桌面端和 `wuu exec` 支持的任务都有明确的运行、完成、失败、中断和恢复状态；
- 受支持工作流中没有已知的高严重度数据丢失或跨工作区隔离问题；
- 用户能够在接受结果前检查重要文件变更和命令活动；
- 模型配置、常见失败、安全边界和自动化契约都有文档，并进入发布检查；
- `app-server` 对外部客户端有明确的兼容策略；
- 安装包通过可复现的产品检查，平台、签名和预览版本限制都被如实说明。

在达到这些标准前，wuu 仍是快速变化的 pre-1.0 项目。

## 怎么维护优先级

- 当一次发布改变产品方向或完成一个主要结果时，复查这份路线图。
- 数据安全、安全漏洞或核心工作流损坏可以覆盖上面的普通顺序。
- 具体工作放在 GitHub issue 中，并写清用户问题、范围和验收标准。路线图只链接 issue，
  不重复其中的任务清单。
- 已完成的工作移入[更新日志](CHANGELOG.md)，不在这里积累成越来越长的勾选列表。

欢迎通过 [GitHub Issues](https://github.com/blueberrycongee/wuu/issues) 提交想法和修正。
