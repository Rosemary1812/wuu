# 路线图

[English](ROADMAP.md)

wuu 仍处于 1.0 之前。当前最重要的事情，是先让已有的编码工作流稳定、可靠、容易检查，
再扩展更大的工作区和多 Agent 能力。

这份路线图只表示方向，不是发版时间表。完整方案和进度以对应 issue 为准；已经发布的
内容见[更新日志](CHANGELOG.md)。

## 当前重点

- **让后台工作的生命周期可以预期。** 后台命令与需要跨 app-server 重启存活的进程目前
  使用互相冲突的归属和恢复规则，用户不容易判断任务是否还活着、是否还能控制。我们希望
  把它们收敛成一套清楚的生命周期。
  ([#157](https://github.com/blueberrycongee/wuu/issues/157))

- **让后台命令更容易复查。** 命令输出已经可以在终端工作区重新查看，但环境面板还不能
  展示当前会话仍存活的后台进程，也不能直接跳转到对应终端资源。
  ([#103](https://github.com/blueberrycongee/wuu/issues/103))

- **补齐环境面板中的仓库状态。** 环境面板目前仍看不全 upstream、PR 和 CI 状态。
  ([#57](https://github.com/blueberrycongee/wuu/issues/57))

- **让模型支持保持更新，也让消耗说得明白。** 内置模型目录在构建时固定，新模型或修正
  信息必须等下一个 wuu 版本。模型服务返回的 token 总量也不能说明哪些请求部分产生了
  新输入。我们希望支持运行时更新目录，并在不保存提示内容的前提下解释消耗来源。
  ([#148](https://github.com/blueberrycongee/wuu/issues/148)、
  [#119](https://github.com/blueberrycongee/wuu/issues/119))

## 后续计划

- **减少从其他编码 Agent 迁移过来的重复配置。** 用户目前需要手动寻找并重新建立已有的
  项目说明、偏好和其他实用设置。wuu 应发现兼容设置，讲清来源和导入位置，并让用户逐项
  选择；不会静默复制凭据，也不会自动启用可执行扩展。
  ([#153](https://github.com/blueberrycongee/wuu/issues/153))

- **给生成产物一个能一直放在对话旁边的位置。** 交互结果目前主要留在消息流中，办公
  文档也没有一等预览工作区。我们希望用户通过聊天持续制作网页、DOCX 和 PPTX 时，右侧
  始终能看到当前产物，同时继续以工作区文件为真实来源。
  ([#154](https://github.com/blueberrycongee/wuu/issues/154)、
  [#20](https://github.com/blueberrycongee/wuu/issues/20))

## 探索方向

这个问题值得解决，但方案还没有排期：

- **当前内置 webview 无法复用用户已有的浏览器资料，也限制了更深的 Agent 集成。**探索带有
  明确凭据和权限控制的完整浏览器工作面。
  ([#96](https://github.com/blueberrycongee/wuu/issues/96))

如果核心缺陷、安全问题或用户反馈表明有更重要的事情，优先级会随之调整。欢迎通过
[GitHub Issues](https://github.com/blueberrycongee/wuu/issues) 提交建议。
