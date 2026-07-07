# 手机端 v1:纯聊天客户端(clients/mobile)

状态:定稿,随实现同步修订。
前置:`2026-07-06-mobile-client-design.md`(架构与范围裁定)。
本文是实现规格:视觉系统、屏幕规格、数据流契约、工程结构、验证策略。

## 0. 一句话

**你的 agent 团队在电脑上干活,你在手机上跟他们聊天。**
手机端只做与电脑上 DM 与群聊线程的对话;不搬项目 session,没有审批卡。

## 1. 视觉系统

产品 App 用桌面端的纸-墨 token 家族(不是 landing 的营销页直角系统),
但继承同一哲学:**底盘极素,识别度只来自少数署名时刻**。

### 1.1 Token(light / dark)

| Token | Light | Dark | 用途 |
|---|---|---|---|
| paper | `#ffffff` | `#1d2024` | 背景 |
| ink | `#1f2328` | `#e4e6e8` | 主文字 |
| inkSoft | `#5b6066` | `#9aa0a6` | 次级文字 |
| inkMuted | `#8a8f94` | `#7d838a` | 弱文字/时间戳 |
| inkFaint | `#b0b6bb` | `#5f656c` | 最弱 |
| overlay4/6/8 | rgba(31,35,40,.04/.06/.08) | rgba(228,230,232,同档) | agent 气泡底/分隔线 |
| accent | `#ff3d00` | `#ff5a26` | **系统里唯一的彩色** |
| accentPress | `#e53600` | `#ff6d3f` | 按下态 |
| userBubble | `#e8e8e4` | `#2a2e33` | 用户气泡底 |
| success / warning | `#1f9d55` / 琥珀 | 同 | 在线点/忙碌点 |

字号对齐桌面:正文 14 + 行高 1.75(移动端 15/1.7 微调可),名字行 12,
时间戳 11。衬线只出现在空态/引导页标题(iOS Georgia / Android serif +
中文回退),权重 460 左右,别处一律无衬线。

### 1.2 署名时刻(每屏最多一个装饰时刻,聊天内容区永远素的)

| 时刻 | 素材(源) | 处理 |
|---|---|---|
| 配对引导 hero | 圆桌团队 concept-27 | 已压 1125w JPEG q82,198KB |
| 配对成功瞬间 | 毕业生 concept-32 | 缩至 480px PNG |
| 会话列表空态 | 面瘫站立 concept-06 + 衬线标题 | 缩至 480px PNG |
| 连接中/重连条 | reaction `eyes` 18px | 原图(16KB) |
| 出错态 | reaction `shrug` | 原图 |
| App 图标/启动屏 | 特写脸 concept-01 | 1024px PNG,白底 |
| 会话头像 | 桌面 mascot-0..11(17-25KB) | 原图直搬,同一哈希算法 |
| 消息表情回应 | 桌面 reactions 6 PNG | 原图直搬,与桌面数据互通 |

### 1.3 关键部件规格(对齐桌面 chat.css)

- **用户气泡**:userBubble 底,圆角 **12/12/3/12**(右下 3px 收角=署名),
  无头像,右对齐,max-width 72%。发送中态 opacity 0.65 + 「发送中…」。
- **Agent 气泡**:overlay4 底,圆角 12 对称,左侧 28px 圆头像 + 上方
  12px 名字行(inkMuted)。DM 与群同构。
- **发送按钮**:32px 正圆,accent 底白 Send 图标;运行中变红色方块「停止」。
- **头像**:avatar_image data URL 优先;否则 FNV-1a(id||name) → %12 定
  背景色(12 档 hsl,light L90%/dark L21%)、%cast 定吉祥物(resident
  → mascot-7..11,其余 → mascot-0..6),contain + 5% padding,圆形裁剪。
- **状态点**:头像右上 8px 圆,1.5px paper 描边;绿=在线,琥珀=正在响应。
- **表情聚合 chip**:pill,overlay4 底 + hairline 边,18px 贴纸 + 计数。

## 2. 屏幕规格(3 屏 + 全局)

### 2.1 配对(Pair)

- Hero:圆桌图 + 衬线标题「你的 agent 团队,随身携带」+ 说明一行。
- 主按钮「扫码配对」(accent 实心)→ expo-camera 扫 `wuu://pair?...`;
  相机不可用/被拒 → 手动粘贴 URI 输入框兜底(始终提供文字入口)。
- 配对中态 → 成功:毕业生插画 + 主机名 +「开始聊天」。
- 凭据存 expo-secure-store(Keychain/Keystore);启动时有凭据直接进列表。

### 2.2 会话列表(Chats)

- 顶栏:主机名(左)+ 连接状态(见 2.4)。
- 分区:置顶 → 其余;行 = 头像(群:最多3个20px叠圆)+ 标题 + 预览
  + 时间 + 未读点(accent 8px)/运行中呼吸点。
- 排序镜像桌面:运行中按 created_at 降序在前,结束按 updated_at 降序。
- 未读:本地 lastViewedTurnByThreadID ≠ latestCompletedTurnID;运行中不标。
- 空态:concept-06 + 衬线「还没有对话」+「在电脑上建一个群,这里就会出现」。
- 长按行:置顶/取消置顶(v1 仅此一项)。**不做**:建群、建 DM(桌面建,手机聊)。

### 2.3 会话页(Thread)

- 顶栏:头像+标题;群显示成员数,点开成员简单列表(名字+状态点,只读)。
- 消息流 = 白名单渲染(见 3.3),inverted FlatList,打开即到底部。
- 已读回执:用户消息侧「已读 n/N」小字(v1 不画环)。
- 表情:长按气泡 → 6 贴纸选择条;聚合 chip 展示;发送 `message/react`。
- Composer:圆角 18 输入框 + accent 圆发送键;placeholder 群「有事直接
  在群里喊」/DM「有事直接跟 {名字} 说」;@ 触发 mention 菜单(群);
  运行中 → 发送变 `turn/queue`(乐观淡色气泡),停止键 `turn/interrupt`。
- 附件 v1 不做(空数组照发)。

### 2.4 全局连接状态

RemoteClient 状态映射为顶部细条:连接中(eyes 表情 + 「连接中…」)/
已断开(「重连中…」)/ attach resumed=false → 全量重拉。前台恢复即重连;
后台不保活(推送是 M2)。

## 3. 数据流契约(从 desktop 实码提取,权威见研究报告)

### 3.1 RPC 面(v1 全集)

`initialize` · `thread/list`(无 params;服务端保证 DM/群全量返回)·
`thread/resume {session_id}`(**result 即全量历史**)· `turn/start
{thread_id, prompt, images:[], files:[], mentions?}`(mentions 仅非空附带;
服务端 DisallowUnknownFields)· `turn/queue {…, client_id}` ·
`turn/dequeue` · `turn/interrupt {thread_id}` · `thread/marks {thread_id}` ·
`message/react {thread_id, seq, reaction}` · `participant/list` ·
`thread/pin {thread_id, pinned}`。

### 3.2 过滤与排序

```ts
isDM    = t => !!t.dm_participant_id && t.workspace_kind === "dm"
isGroup = t => t.group === true
visible = t => !t.archived && !t.read_only
// 排序:运行中(status==="in_progress"||turns 有 in_progress)按 created_at
// 降序在前;其余按 updated_at||created_at 降序。置顶单独分区。
```

### 3.3 聊天白名单(镜像 AppState.chatMessagesFromTurns)

按序判定:focus_meta → 分隔行;user_message 且 handoff JSON → 丢弃;
user_message 且 envelope_meta → 信封折叠行(相邻合并);user_message →
用户气泡;participant_message 且 post_kind==="decline" → 「{名字} 认为
无需回应:{text}」灰行;participant_message → agent 气泡(participant
归属);task_card → 折叠任务卡(名称+状态徽标+N 条回复);**其余一切
不渲染**(agent 工作转录零噪音)。

### 3.4 通知 reduce(会话列表 + 打开的会话)

`thread/started|resumed|updated` → upsert(turns 空时保留本地);
`turn/started` → 运行中(+移除 queue_id 对应乐观气泡);
`turn/completed|error` → 结束+重排+未读判定;`item/started|completed`
→ 按 id upsert(**短快照不得截断已有长文本**);`item/*/delta|replace`
→ 文本增量/替换;`message/mark` → (seq,participant_id,kind) upsert;
`participant/updated` → 重拉 participant/list。其余安全忽略。

### 3.5 @mention

`@名字` 原样留在 prompt;整词正则 `(^|\s)@Name(?=$|\s|[,.!?，。；：、])`
按名字长度降序对 roster 解析出 id 数组放 `mentions`(仅非空)。

## 4. 工程结构

```
clients/mobile/
  app.json  metro.config.js  babel.config.js  tsconfig.json  package.json
  assets/…(§1.2 的 8 类)
  src/theme.ts
  src/lib/    connection.ts(RemoteClient 接线+凭据) store.ts(通知 reducer,
              useSyncExternalStore) chatModel.ts(白名单,纯函数)
              avatar.ts(FNV-1a,纯) mentions.ts(纯) format.ts
  src/screens/ PairScreen  ChatsScreen  ThreadScreen
  src/components/ Avatar Bubble ReactionBar TaskCardRow EmptyState
                  ConnectionBanner MemberStack
  test/       chatModel/store/avatar/mentions 的 vitest
  App.tsx(hand-rolled 三屏导航或 native-stack,实现时定)
```

依赖:expo + react-native + react;expo-camera / expo-secure-store /
expo-status-bar;react-native-get-random-values(remote-core 随机源);
`@wuu/remote-core`(file:../core)与 `@wuu/protocol`(file:../../packages/
protocol);dev:typescript、vitest、react-native-web + react-dom(export
web 验证)。metro:watchFolders 指 repo 根 + `unstable_enablePackageExports`
(两个内部包都是 exports 指 TS 源)。

## 5. 验证策略(无头环境)

1. `tsc --noEmit`;
2. vitest:chatModel 白名单(含 handoff 丢弃/信封合并/decline)、store
   reducer(排序/未读/乐观气泡移除/marks upsert)、avatar 哈希钉值、
   mention 解析边界;
3. `expo export --platform web`:全量打包必须过(monorepo 解析的真验证);
4. 真机冒烟(用户侧):`wuu relay` + 桌面开远程 → 手机扫码 → 列表可见
   DM/群 → 发消息收到回复。

## 6. 实现顺序(原子提交)

1. 资产准备(压缩/搬运)+ Expo 脚手架 + monorepo 打通(export web 过)
2. lib 层:connection/store/chatModel/avatar/mentions + vitest 全绿
3. 三屏 UI + 组件(theme 落地)
4. 收尾:图标/启动屏/文档进展 + 多 agent 审查修复
