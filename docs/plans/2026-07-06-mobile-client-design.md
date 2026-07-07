# 移动端(iOS / Android)与 Web 客户端架构设计

状态:已定稿(2026-07-06 决策拍板,见 §9)。
前置阅读:`docs/remote-control-zh.md`(远程链路)、`docs/app-server-protocol.md`(协议面)。

## 0. 背景与产品目标

远程链路(`internal/remote/*`)已于 2026-07-06 落地并通过端到端集成测试:
设备配对、盲中继、签名握手、密封通道、断线续传、离线推送钩子全部就绪,
手机侧客户端库(`internal/remote/phone`)以 CLI 形态验证过真机冒烟。

本文设计链路之上的**人用的客户端**:

> 人在外面,用手机看到电脑上正在跑的 agent、给它派活、在它等待批准时放行。

三个核心场景,按价值排序:

1. **审批放行**:agent 碰到权限门 → 手机收推送 → 打开审批卡 → 批准/拒绝,
   电脑上的 agent 继续跑。这是远程操控区别于"又一个聊天 App"的根本价值。
2. **派活与追踪**:发起新 turn、流式看输出、中断、切线程。
3. **状态一览**:哪台电脑在线、哪些 agent 在跑、跑到哪了。

## 1. 渠道策略

**结论:iOS + Android 双端上架为第一波;Web 为第二波,但架构上按三端同源设计,
让 Web 的边际成本接近于零。**

| 渠道 | 定位 | 关键依赖 |
|---|---|---|
| iOS / Android App | 主力。推送唤醒、摄像头扫码、系统级密钥保管(Keychain / Keystore) | 商店上架流程 |
| Web | 补充。"在任何浏览器里临时看一眼/批一下",亦是自架用户的零安装入口 | 密钥保管弱于移动端(见 §6) |

Web 值得做但不该先做的原因:

- 推送(离线唤醒)是审批场景的命脉,Web Push 的到达率与体验都显著弱于 APNs/FCM;
- 浏览器里的长期设备密钥只能落 IndexedDB/WebCrypto,保管强度低一档;
- 配对扫码在手机上是自然动作,在浏览器里是别扭动作。

自架故事:relay 后续可直接托管 Web 客户端静态资源,自架用户 `wuu relay` 起来后
浏览器打开同一地址即得完整入口,零额外部署。

## 2. 技术栈决策

核心问题只有一个:**协议核心(密码学握手 + 密封通道 + 断线续传)用什么语言承载。**
UI 框架的选择从属于这个答案。

### 已确认的事实

- 桌面端已有完整 TS 协议类型层 `desktop/src/shared/protocol.ts`
  (1880 行、163 个导出),覆盖全部 Request/Response/Notification 类型,
  被 Electron UI 实战验证。
- 手机要说的就是这套协议,一字不改(远程层的设计承诺)。
- 需要新写的只有加密与续传层:relay 腿鉴权、配对、握手、密封通道、
  attach/resume + ack。Go 参考实现约 2k 行,且有完整测试锁定线格式。
- React Native 自带 WebSocket(含二进制帧);Ed25519 / X25519 / HKDF / AES-GCM
  在 `@noble/curves`、`@noble/hashes`、`@noble/ciphers`(纯 JS、经安全审计)中齐备。
  本链路的流量是聊天尺度(行级 JSON),纯 JS 密码学性能绰绰有余;
  将来需要提速可平替 JSI 原生实现,接口不变。

### 方案对比

| 方案 | 协议核心 | 覆盖 | 优势 | 代价 |
|---|---|---|---|---|
| **A. React Native(Expo)+ TS 协议核心(推荐)** | TS 重写加密层,协议类型直接复用 | iOS + Android + Web 三端一份核心 | 与 desktop 同语言同心智,类型层白拿;Expo 迭代最快(EAS 构建、OTA 更新);Web 近乎免费 | 加密层跨语言重写(~2k 行),需测试向量护航 |
| B. React Native + Go 绑定 | 直接绑定 `internal/remote/phone` | iOS + Android;Web 需另做 WASM 胶水 | 零密码学重写 | 交叉编译工具链 + 原生模块胶水,放弃 Expo 托管迭代;两套产物(绑定库 + WASM)维护 |
| C. Flutter(+ FFI 绑定) | 同 B | 三端+ | 单 UI 栈 | 全新 UI 语言,现有 React 设计体系、组件模式、协议类型零复用 |
| D. 双原生(Swift + Kotlin) | 每端各接绑定或各重写 | 双端 | 平台保真度最高 | 两套 UI 两套胶水,当前团队规模不现实 |

### 结论:方案 A

决定性理由是**复用密度**:desktop 的协议类型、状态管理模式(通知流 + RPC 拉取)、
会话 UI 的组件心智全部平移;团队(人 + agent)已在这套栈上高强度作业数周。

方案 A 的唯一实质风险是加密层跨语言重写。缓解措施(M0 里程碑,先行落地):

- Go 侧新增测试向量导出:固定种子生成配对往返、握手 transcript、
  密封帧(含 nonce 序列与 AAD)的确定性 JSON 向量;
- TS 实现必须逐字节复现全部向量,并加双向互通测试
  (Go host ↔ TS phone 各当一端);
- Go 实现永远是权威参考,线格式变更先改 Go 与向量、再同步 TS。

## 3. 仓库与包结构

```
clients/
  core/        # @wuu/remote-core:纯 TS,零 UI 依赖,三端共用
  mobile/      # Expo RN App(iOS + Android)
  web/         # 第二波:React Web,复用 core 与移动端的组件逻辑层
packages/
  protocol/    # 从 desktop/src/shared/protocol.ts 提升出来的协议类型包
```

前置小重构:把 `desktop/src/shared/protocol.ts` 提升为 workspace 包
`packages/protocol`,desktop 与 clients 共同消费,单一事实源。

### `@wuu/remote-core` 内部分层

```
transport/   relay 腿:WebSocket、挑战-响应鉴权、帧收发、指数退避重连
pairing/     解析配对 URI、ECDH 密封 offer、校验 answer、凭据持久化接口
secure/      签名临时 DH 握手、方向密钥派生、计数器 nonce 密封通道
session/     attach/resume 状态机、seq 应用与 ack、at-most-once 上行语义
rpc/         行分隔 JSON-RPC 客户端(与 desktop 同款方法面)
state/       HostState / 运行中 turn / 线程缓存,事件流出口
```

约束:core 不 import 任何 RN / DOM API;凭据存取、WebSocket 构造器、
推送令牌均由宿主注入(依赖倒置),保证同一份代码跑 RN 与浏览器。

## 4. App 信息架构(v1)

```
主机列表(多电脑支持,显示在线状态与运行中 turn 数)
 └─ 线程列表(与 desktop 同一 RPC:thread/list,含群聊线程)
     └─ 会话视图(流式输出、发 turn、中断、审批卡内联展示)
审批中心(跨主机聚合所有待批请求,推送落点)
扫码配对(摄像头 + 手动粘贴 URI 兜底)
设置(设备管理、中继地址、通知偏好、生物识别开关)
```

状态管理与 desktop `AppState` 同构:通知流增量更新 + RPC 全量拉取兜底,
attach 时 `resumed:false` 即触发全量重拉。

## 5. 移动端现实约束(链路已为此设计,App 层要接住)

- **后台 socket 必死**:iOS/Android 均会在后台数十秒内杀长连接。语义已就绪——
  电脑侧 turn 照跑,spool 缓冲下行;App 回前台重连 attach,精确重放或全量重建。
  App 层要做的只是"前台即重连"与重放期间的 UI 静默合并。
- **推送是唤醒通道而非内容通道**:推送体永远无内容(仅 `needs_input` /
  `agent_done` 枚举),点开后靠重连拉真实状态。既是隐私设计也规避了
  推送体积与审查风险。
- **上行 at-most-once**:断线瞬间的发送以显式失败呈现("未送达,点击重试"),
  不做自动重发,避免 turn/start 重复执行。幂等键落地后再改自动重试。

## 6. 密钥与安全

| 项 | 移动端 | Web |
|---|---|---|
| 设备身份种子 | Keychain / Keystore(expo-secure-store),不出安全区 | WebCrypto 不可导出密钥 + IndexedDB;明确弱一档,文档如实告知 |
| 审批操作 | 可选生物识别门(Face ID / 指纹后才能点批准) | 无 |
| 凭据撤销 | 桌面端设备管理页可随时吊销;host 拒绝已吊销设备的握手 | 同 |

## 7. 依赖的 host / core 侧前置工作

| # | 工作 | 层 | 规模 | v1? |
|---|---|---|---|---|
| 1 | 桌面设置页:远程开关 + 配对二维码面板 + 已配对设备管理 | desktop | 小 | ✅ 必须 |
| 2 | 审批反向请求接入远程通道(host 侧虚拟连接启用 requestClient) | core/host | 中 | ✅ 必须(核心价值) |
| 3 | relay 增设备推送令牌注册消息 + APNs/FCM 推送网关 | relay | 中 | ✅ 必须(审批场景命脉) |
| 4 | Go 侧测试向量导出(M0 护航) | remote/secure | 小 | ✅ 必须 |
| 5 | app-server 多订阅连接(与 desktop 实时互看同一 turn 的流式增量) | core | 大 | ❌ 后续(v1 语义:可 resume、可驱动、下一 turn 可见) |
| 6 | 上行幂等键(自动安全重试) | host/phone | 小 | ❌ v1.1 |

## 8. 里程碑

- **M0 地基**:测试向量导出;`packages/protocol` 提升;`@wuu/remote-core`
  实现并通过全部向量 + 双向互通测试。
- **M1 可用**:Expo App 跑通扫码配对 → 线程列表 → 发 turn → 流式输出 → 中断;
  桌面二维码面板(前置 #1)同步落地。
- **M2 价值闭环**:审批卡(前置 #2)+ 推送(前置 #3)+ 生物识别门。
  此时产品故事完整:人在外面,agent 不再等你回家。
- **M3 上架与外溢**:双端商店上架;Web beta(clients/web,复用 core);
  relay 托管 Web 静态资源。

## 9. 已定决策(2026-07-06)

1. 技术栈:**方案 A**(React Native + Expo,纯 TS 协议核心,测试向量护航)。
2. Web:**第二波(M3)**,架构保持三端同源。
3. 实时互看:**v1 不做**,接受"共享持久层"语义;app-server 多订阅列为后续 core 改造。

## 10. 落地进展(2026-07-07)

M0 全部完成,M1 前置部分完成:

- ✅ 测试向量导出(§7 #4):`internal/remote/secure/testdata/vectors.json`,
  Go 测试逐字节钉死线格式,`-update` 再生成。
- ✅ `packages/protocol` 提升:desktop 内 141 处 import 零改动。
- ✅ `@wuu/remote-core`(clients/core):加密层双侧实现并通过全部向量
  (双向逐字节);45 项测试覆盖行为安全与客户端语义(假中继+假主机:
  断线精确重放、spool 丢失降级、审批反向请求、累计 ack、at-most-once)。
- ◐ 桌面配对面板(§7 #1):`remote status/devices --json` 与
  `devices remove` CLI、主进程 RemoteHostManager(托管 remote host
  子进程、捕获配对 URI、吊销即重启)、SettingsRemotePage 组件与样式
  均已落地;SettingsView 导航与 preload/main IPC 接线与在途的皮肤
  重构同文件,待其落地后以小改动补上。
- 未动:审批反向通道(§7 #2)、推送(§7 #3)——M2 内容。
