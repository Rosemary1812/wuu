# 远程设备与 Relay

wuu 的 remote 能力让一台电脑作为 Host，通过可自托管的 Relay 为已配对的手机或其他
客户端提供远程控制。Relay 只负责转发加密连接；Host 仍在指定工作区中运行 wuu，模型
配置、权限和文件访问边界仍由 Host 决定。

## 初始化 Host

先为当前用户创建远程身份。远程状态默认保存到 `~/.wuu/remote.json`；设置 `WUU_HOME`
时会随用户目录一起移动。

```bash
wuu remote init --relay wss://relay.example.com/v1/connect --name my-mac
```

`--relay` 可省略，之后再运行初始化命令补上；命令会输出远程身份指纹，配对或排查时
可用它确认连接的是正确 Host。

## 启动 Host 并配对手机

在需要被远程控制的电脑上启动 Host：

```bash
wuu remote host --workdir /path/to/project --pair
```

终端会打印配对 URI。把 URI 转成二维码或直接复制到手机端，配对窗口默认持续 10 分钟，
首次设备配对后自动关闭。需要调整行为时可使用：

```bash
wuu remote host --pair --pair-timeout 30m --pair-once=false
```

也可以在启动 Host 时覆盖 provider、模型、工作区或 Relay：

```bash
wuu remote host \
  --workdir /path/to/project \
  --provider openai \
  --model gpt-5.6 \
  --relay wss://relay.example.com/v1/connect
```

Host 进程必须保持运行；停止 Host 或 Relay 不会删除已保存的配对身份。

## 管理已配对设备

查看 Host 身份、Relay 和设备数量：

```bash
wuu remote status
wuu remote status --json
wuu remote devices
wuu remote devices --json
```

撤销设备时使用显示的指纹：

```bash
wuu remote devices remove <fingerprint>
```

撤销会阻止新的握手；正在运行的 Host 可能需要重新加载或重启后才会完全采用最新设备
列表。不要把完整公钥、配对 URI 或 `remote.json` 发布到 Issue 或聊天中。

## 手机端命令

手机端先导入配对 URI：

```bash
wuu remote phone pair --uri 'wuu://pair?...'
```

随后可以查看连接状态、发送任务并监听事件：

```bash
wuu remote phone status
wuu remote phone send "查看当前工作区的测试状态"
wuu remote phone watch
```

手机端状态文件默认位于用户目录；需要隔离多个身份时，用 `--store FILE` 指定单独文件。
发送任务的权限仍由 Host 的运行时和工作区策略决定，手机端不会绕过只读模式或敏感路径
保护。

## 故障排查

- **无法连接 Relay：**检查 `ws://`/`wss://` 地址、端口、防火墙和 Relay 的 `/v1/connect`
  路径；Host 与手机必须使用可达的同一 Relay。
- **配对 URI 过期：**重新启动 `wuu remote host --pair` 生成新的窗口和 URI。
- **设备显示已配对但无法操作：**先运行 `wuu remote status`，再确认 Host 进程仍在运行、
  工作区存在且模型服务凭据可用。
- **任务能启动但结果中断：**查看手机端 `watch` 输出和 Host 日志；不要重复发送同一任务，
  先确认上一轮是否仍在运行。

远程功能涉及公网连接时，请为 Relay 配置 TLS、访问控制和日志脱敏；不要把 Relay 当作
模型服务或凭据存储。

## 相关文档

- [模型服务](../getting-started/model-services.md)：配置 Host 使用的 provider 和模型；
- [权限模式](../reference/permissions.md)：了解 Host 可读写的路径和命令边界；
- [App-server 集成](app-server.md)：构建其他客户端时使用核心协议。
