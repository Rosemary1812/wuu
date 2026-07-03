# 侧栏群聊 / Andy / 工作区范围 — 人工验收脚本（S8）

对应设计：`2026-07-03-sidebar-groups-andy-workspaces.md`（S4–S7 后端已落地）。
按步骤走一遍全新安装的首启体验；每步给出预期结果。
标注 ✅machine 的步骤已由实施 agent 用 `wuu debug app-server send` 协议探针
在全新 state 下机器验证过（2026-07-03，commit 614a5917 后），人工可抽查。

## 准备：全新 state

1. 备份并清空本机 state（或用一次性 HOME）：

   ```bash
   export SANDBOX=$(mktemp -d)
   mkdir -p "$SANDBOX/work"
   export HOME="$SANDBOX" WUU_HOME="$SANDBOX/.wuu"
   ```

2. 配置一个可用的 provider（`~/.config/wuu/config.json` 填真实 API key），
   否则 Andy 的常驻 turn 无法真正推理（第 5 步起需要）。

## 1. 首启 → Andy 存在 ✅machine

启动桌面 app（或探针）：

```bash
go run ./cmd/wuu debug app-server send --workdir "$SANDBOX/work" participant/list
```

预期：

- roster 恰有一个 named agent：`name: "Andy"`、`avatar: 🦉`、
  `tagline: 帮你把合适的 agent 拉进合适的房间`。
- `$WUU_HOME/default-agent-seeded` 标记文件存在。
- `$WUU_HOME/participants/<id>/MEMORY.md` 含预置人设（`角色：团队组建者`）。

删除测试（不复活）：在 roster 里删除/退休 Andy，重启 app →
Andy 不再出现（标记 + 参与者计数双重保证）。

## 2. `# all` 频道存在且受保护 ✅machine

```bash
go run ./cmd/wuu debug app-server send --workdir "$SANDBOX/work" thread/list
```

预期：列表含 `title: "all"`、`group: true` 的 thread，`members` 镜像全体
named agent（此时即 Andy 一人）。对它调用 `thread/rename` / `thread/archive`
应返回 `cannot rename/archive the built-in all channel` 错误。

## 3. 建群入口 ✅machine

```bash
go run ./cmd/wuu debug app-server send --workdir "$SANDBOX/work" \
  thread/start '{"group":true,"title":"launch"}'
```

预期：返回 `group: true`、`title: "launch"` 的 thread；侧栏群聊 section
出现 `# launch`。

## 4. `# all` 首聊：turn 立即完成、不跑模型 ✅machine

```bash
go run ./cmd/wuu debug app-server send --workdir "$SANDBOX/work" \
  turn/start '{"thread_id":"<all 的 id>","prompt":"大家好，先自我介绍一下"}'
```

预期：响应里 turn `status: "completed"`（同步完成，无 assistant 输出——
群 thread 没有主 agent）。

## 5. Andy 被唤醒并自我介绍（需真实 provider，人工验证）

上一步的消息会以信封进入 Andy 的常驻 DM thread。预期（桌面 app 里观察）：

- Andy 的 DM thread 出现一条折叠的"收到来自「all」的消息"。
- Andy 按人设在 `# all` 里 `post_message`：自我介绍 + 询问目标 +
  提议一个最小团队配置（不会静默，因为 `# all` 首聊对它是 addressed
  语义由人设指引；若静默属提示词回归）。

## 6. Andy 建群拉人（需真实 provider，人工验证）

在 `# all` 或 Andy 的 DM 里说："帮我组一个做 X 的小团队，建个群"。

预期：

- Andy 用 `manage_participant` 创建 1–2 个 named agent（宁缺毋滥）。
- 用 `create_group` 建一个群（侧栏群聊 section 出现新群；Andy 是成员）。
- 用 `add_group_member` 把新成员拉进群（群 members chips 出现他们）。
- 预算行为：单 turn 内 Andy 最多建 1 个群、拉 8 人；单群成员上限 8。
  （预算与上限已有 Go 单测覆盖：`resident_groups_test.go`。）

## 7. 工作区文件范围（部分 ✅machine，通过 Go 测试覆盖）

1. 侧栏"添加工作区"加一个项目目录（写入 `$WUU_HOME/projects.json`）。
2. 在 Andy 的 DM 里让它读该项目里的某个文件 → 成功。
3. 让它读工作区之外的路径（如 `/etc/hosts`）→ 工具报错，错误信息含
   "该路径不在工作区内，请用户在侧栏添加该目录为工作区后重试"。
4. Andy 的 system prompt 应含 "## Workspaces and file scope" 段并列出
   工作区（`- {名字} — {路径}`；无工作区时 `(none yet)`）。
   机器覆盖：`participant_prompt_test.go`、`tool_filescope_test.go`、
   `resident_filescope_test.go`。
5. 普通项目 work session 的文件工具行为不变（仍限项目目录）。

## 机器验证记录（2026-07-03）

- ✅ 全新 state 首启 `participant/list` 只有 Andy（名字/tagline/人设
  MEMORY.md/marker 均符合）。
- ✅ `thread/list` 出现 `# all`（group=true，members=[Andy]）。
- ✅ `thread/start {group:true,title:"launch"}` 创建群 thread。
- ✅ `# all` 上 `turn/start` 同步 completed、无 provider 调用。
- ⬜ 第 5、6 步需要真实模型 key，未机器验证（信封路由、budget、
  文件范围逻辑均有 Go 测试覆盖）。
