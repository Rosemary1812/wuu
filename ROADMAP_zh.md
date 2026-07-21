# 路线图

[English](ROADMAP.md)

wuu 仍处于 1.0 之前。当前最重要的事情，是先让已有的编码工作流稳定、可靠、容易检查，
再扩展更大的工作区和多 Agent 能力。

这份路线图只表示方向，不是发版时间表。具体范围和进度以对应 issue 为准；已经发布的
内容见[更新日志](CHANGELOG.md)。

## 当前重点

| 方向 | 目标 | 跟踪 |
|---|---|---|
| 运行时 | 让后台命令、中断、取消和恢复遵循一套清楚的生命周期 | [#157](https://github.com/blueberrycongee/wuu/issues/157)、[#31](https://github.com/blueberrycongee/wuu/issues/31) |
| 变更 | 让 Agent 修改和命令活动容易检查，同时避免撑大模型上下文 | [#151](https://github.com/blueberrycongee/wuu/issues/151)、[#103](https://github.com/blueberrycongee/wuu/issues/103) |
| 桌面端 | 补齐常用的文件、Git、定时任务、PR 和 CI 工作流 | [#130](https://github.com/blueberrycongee/wuu/issues/130)、[#135](https://github.com/blueberrycongee/wuu/issues/135)、[#57](https://github.com/blueberrycongee/wuu/issues/57)、[#56](https://github.com/blueberrycongee/wuu/issues/56) |
| 模型服务 | 及时更新兼容模型信息，并让请求消耗更容易理解 | [#148](https://github.com/blueberrycongee/wuu/issues/148)、[#119](https://github.com/blueberrycongee/wuu/issues/119) |

## 后续计划

- 安全复用其他编码 Agent 的兼容设置和项目资产，不静默复制凭据，也不自动启用可执行扩展
  ([#153](https://github.com/blueberrycongee/wuu/issues/153))。
- 建立面向代码、网页预览、DOCX 和 PPTX 的产物工作区，同时继续以文件为真实来源
  ([#154](https://github.com/blueberrycongee/wuu/issues/154)、
  [#20](https://github.com/blueberrycongee/wuu/issues/20))。
- 继续加强 `app-server`，让桌面端、自动化和未来客户端复用同一套核心契约。

## 探索方向

这些想法还没有排期：

- 供人和 Agent 共同使用的代码库知识空间
  ([#36](https://github.com/blueberrycongee/wuu/issues/36))。
- 基于任务图和具名 Agent 的协作方式
  ([#138](https://github.com/blueberrycongee/wuu/issues/138))。
- 支持受控凭据复用的深度浏览器工作面
  ([#96](https://github.com/blueberrycongee/wuu/issues/96))。
- 更多客户端 Shell，以及更广的桌面安装包平台支持。

如果核心缺陷、安全问题或用户反馈表明有更重要的事情，优先级会随之调整。欢迎通过
[GitHub Issues](https://github.com/blueberrycongee/wuu/issues) 提交建议。
