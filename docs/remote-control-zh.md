# 远程控制:手机操控电脑上的 agent

本文描述 wuu 的远程控制链路(2026-07-06 落地):人在外面用手机查看、接管电脑上正在跑的
agent。使用场景对标 OpenAI Codex 的 remote control 与 slopus/happy,但信任模型取二者之长:
通道复用与可靠投递参照 codex,端到端加密与开源可自架的盲中继参照 happy。

代码位置:

| 组件 | 路径 | 职责 |
|---|---|---|
| 加密核心 | `internal/remote/secure` | 设备身份、配对交换、连接握手、密封通道 |
| 线协议 | `internal/remote/wire` | relay 腿与端到端腿的全部消息类型 |
| 中继 | `internal/remote/relay` | 盲路由、挑战鉴权、配对信箱、在线状态、推送钩子 |
| 电脑侧 | `internal/remote/host` | 身份/设备存储、relay 连接、配对窗口、每手机一条 app-server 连接、spool 重放 |
| 手机侧 | `internal/remote/phone` | 配对、重连、attach/resume、复用 `exec.ProtocolClient` 说 app-server 方言 |
| CLI | `cmd/wuu/remote.go` | `wuu relay` 与 `wuu remote init/host/devices/phone` |

## 总体拓扑

```
手机(未来的 App;现在是 phone 包 / wuu remote phone)
   |  wss,端到端密文(relay 不可读)
   v
wuu relay(可自架,盲中继:只见路由元数据与密文)
   ^
   |  wss,桌面侧主动外连(NAT 友好)
电脑 wuu remote host(与 wuu app-server 同款 runtime)
   每台已配对手机 -> io.Pipe + appserver.RunStdio 的一条独立虚拟连接
```

核心取舍:手机说的就是 desktop 与 core 之间那套行分隔 JSON 协议(`internal/appserver`
的 Request/Response/Notification/ServerRequest),一字不改。远程层只做三件事:鉴权配对、
加密搬运字节、断线续传。app-server 零改动。

## 信任模型与密码学

relay 被设计为不可信:它看得到谁连了谁、时间与大小,看不到任何应用内容。

- 设备身份:每台设备(host 与手机)一把长期 Ed25519 密钥。公钥即 relay 上的路由地址。
- relay 鉴权:挑战-响应。relay 发 32 字节随机 nonce,设备用私钥对
  `nonce || pub || role` 签名(role 绑进签名,手机凭据不能冒充 host)。修掉了 happy
  客户端自造 challenge 的弱点。
- 配对(信任建立):host 生成一次性 X25519 临时钥,连同 host 长期公钥、relay 地址、
  配对 ID 编成 `wuu://pair?...` URI(桌面上以二维码展示)。临时公钥只出现在屏幕上,
  不经过 relay——持有它就是"看到了电脑屏幕"的能力证明,relay 拿到配对 ID 也无法冒充
  任何一方。手机用该临时钥做 ECDH,把自己的设备公钥密封进 offer;host 解开后登记设备、
  回签名 answer;手机校验 answer 里的 host 公钥与二维码钉扎的一致。比 happy V2 更强:
  双向都在配对时拿到对方经认证的长期公钥。
- 连接握手:每次(重)连跑一轮签名临时 DH(SIGMA 式):hs1/hs2 各带新鲜 X25519 临时钥
  与 Ed25519 签名,HKDF 派生两个方向独立的 AES-256-GCM 密钥,具备前向保密。
- 通道:每帧 `nonce(12B) || GCM 密文`,nonce 为方向前缀加严格递增计数器,AAD 绑定握手
  transcript;重放、乱序、跨会话帧一律拒收。
- 全部原语来自 Go 标准库(ed25519、crypto/ecdh、crypto/hkdf、AES-GCM),零新增依赖。

## 可靠性:spool 与 attach/resume

移动网络的常态是断。断线的语义:

- host 侧每台手机持有一条常驻 app-server 连接(`io.Pipe` + `RunStdio`,与
  `internal/exec` 驱动 app-server 的方式同构)。手机掉线不影响连接与其上的 turn:
  agent 继续跑。
- host 给每条下行 rpc 行编 seq 并写入 spool(有界重放缓冲,默认 8192 行 / 32MB);
  手机周期性回 ack,spool 随之裁剪。
- 手机重连后重新握手,发 `attach{prev: 连接续期 ID, recv: 已应用的最高 seq}`:
  - spool 能接上:`attached{resumed:true}`,host 从 recv+1 重放,一行不丢;
  - 接不上(缓冲溢出、host 重启、续期 ID 不符):`attached{resumed:false}`,手机拿到
    全新 app-server 连接,走 RPC 重拉状态(initialize、thread/list、历史)。
- 上行(手机到 host)请求刻意不重放:at-most-once,断线丢失表现为调用报错,由上层
  重试,避免 turn/start 这类副作用请求重复执行。幂等键是后续工作。

codex 的对应物是 BoundedOutboundBuffer + 后端 cursor 重放;我们把它整个放在端到端层,
relay 因此无需持有任何明文或重放状态。

## 状态同步

- host 在 attach 时和运行集变化时推 `state` 帧:host 元信息(名字、版本、workdir、
  provider/model)加正在运行的 turn 列表(通过嗅探下行 `turn/started` /
  `turn/completed` / `turn/error` 通知维护,零 core 改动)。
- 其余状态(线程列表、历史、成员……)手机直接用 app-server RPC 拉,与 desktop 同一
  套方法,天然一致。
- 持久化共享:远程驱动的 turn 落在同一个 SQLite 会话库里,desktop 打开即见。

## 推送

手机离线期间,若下行出现 ServerRequest(agent 等输入)或 `turn/completed` /
`turn/error`,host 经 relay 发**无内容**推送提示(仅 `needs_input` / `agent_done`
一类枚举),host 侧与 relay 侧各有节流。relay 的 `Pusher` 接口默认打日志,可配
`--push-webhook` 转发到任意通知服务;APNs/FCM 接入留作后续。这修掉了 happy 把
"Claude 想执行 X 工具"明文塞进推送的泄漏。

## CLI 用法(当前形态)

```
# 任意一台可达的机器(或本机)起中继
wuu relay --addr 127.0.0.1:8787

# 电脑侧:一次性初始化,然后带配对窗口起 host
wuu remote init --relay ws://127.0.0.1:8787/v1/connect
wuu remote host --workdir ~/project --pair     # 打印 wuu://pair?... URI
wuu remote devices

# "手机"(开发期用 CLI 模拟;未来是移动 App)
wuu remote phone pair --uri "wuu://pair?..." --store ~/phone.json
wuu remote phone status --store ~/phone.json
wuu remote phone send --store ~/phone.json --prompt "修一下登录页的报错"
wuu remote phone watch --store ~/phone.json
```

存储:host 身份与已配对设备在 `<wuu home>/remote.json`(0600),relay 注册表默认
`<wuu home>/relay-state.json`,手机凭据默认 `<wuu home>/phone.json`。

## 验证

- `internal/remote/secure`:配对往返、握手、冒充 host / 未配对设备 / 篡改 / 重放拒收。
- `internal/remote/relay`:挑战鉴权、帧路由、presence、配对信箱、注册表持久化、推送节流。
- `internal/remote/host`:spool 裁剪/缺口/溢出语义;
  `TestRemoteControlEndToEnd` 集成测试跑通全链路:真 relay(httptest)+ 真 host
  (fake provider 的真 runtime + 真 app-server)+ 真 phone 客户端——配对、attach、
  远程 turn 流式完成、持久化落库断言、断线中 turn 完成、离线推送、重连 resume 精确重放。
- 真机冒烟:`wuu relay` + `wuu remote host`(live MiniMax-M3 配置)+
  `wuu remote phone send`,手机侧流式收到模型回复并正常收尾。

## 已知限制与后续

- **与 desktop 的实时互看**:远程手机与 desktop 各自持有独立的 app-server 连接
  (in-memory thread 状态互不广播),共享的是持久层——手机可以 resume desktop 建的
  线程并驱动它,但看不到 desktop 正在流式输出的那个 turn 的实时增量(反之亦然)。
  根治需要 app-server 支持多订阅连接(参照 codex 的 transport 多路复用),是下一步
  的 core 改造;届时 host 侧只需把"每手机一条 RunStdio"换成"多客户端共享连接"。
- 手机上行请求无幂等键,at-most-once;desktop 设置页的启用开关与二维码面板未接
  (CLI 已可用);审批卡依赖 app-server 侧 `requestClient` 反向请求通道启用后自然
  流经本链路(手机端 `ProtocolClient` 已支持应答 ServerRequest)。
- relay 单实例、注册表 JSON 文件;多实例与横向扩展(参照 happy 的 Redis 事件总线)
  未做。帧级别未做分片(app-server 单行 4MB 上限内直接过),超大行依赖 32MB 帧上限
  兜底,codex 式 segment 分片留作后续。
- 本环境无 C 编译器,race detector 未跑(以 -count=5 重复运行代偿);上真实 CI 后
  应补 `go test -race ./internal/remote/...`。
