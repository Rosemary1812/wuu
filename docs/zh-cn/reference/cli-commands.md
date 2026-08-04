# CLI 常用命令

`wuu` 命令行适合在终端中初始化配置、运行任务、恢复会话和检查执行记录。运行
`wuu --help` 可查看当前版本完整帮助；本文只列出面向日常使用、已稳定提供的入口。

## 初始化和检查模型

在首次使用 CLI 的机器上初始化用户目录和默认配置：

```bash
wuu init
```

已有配置时不要随意覆盖；需要明确重建时才使用 `--force`：

```bash
wuu init --force
```

查看配置中可用的模型服务和模型：

```bash
wuu models
wuu models --json
wuu models --provider <provider-name>
```

`models` 只读取配置并输出可用模型，不会启动 Agent 回合。需要指定项目配置时可以加
`--workdir <目录>`。

## 运行、恢复和分叉任务

最常用的入口是 [`wuu exec`](../automation/exec.md)：

```bash
wuu exec --workdir /path/to/project "运行测试并修复失败项"
```

最近一次会话可以继续：

```bash
wuu exec --continue "继续处理刚才的问题"
```

也可以使用线程 ID 精确恢复，或从原会话创建分支：

```bash
wuu exec resume <thread-id> "继续这个会话"
wuu exec fork <thread-id> "尝试另一种实现方案"
```

`wuu -c` 和 `wuu -r <thread-id>` 是 `wuu exec` 对应选项的顶层快捷方式。

## 查看和管理会话

```bash
wuu session list
wuu session show <thread-id>
wuu session search "关键词"
wuu session trace <thread-id>
```

常用用途：

- `list`：列出当前工作区的会话；
- `show`：查看会话的基本信息；
- `search`：按标题或历史内容检索；
- `trace`：查看工具调用和回合事件，适合排查任务为什么失败。

需要脚本处理时，支持 `--json` 的子命令应优先使用 JSON 输出，而不要解析人类可读文本。

归档会话会把它从默认列表隐藏，但不会删除记录：

```bash
wuu session archive <thread-id>
```

删除会话及其工作区产物前请确认 ID：

```bash
wuu session delete <thread-id>
```

导出会话历史为 JSONL 文件：

```bash
wuu session export <thread-id> --out conversation.jsonl
```

## 评审改动

可以让 Agent 直接评审当前未提交改动、某个基线或某次提交：

```bash
wuu exec review --uncommitted
wuu exec review --base main
wuu exec review --commit <commit-sha>
```

评审仍遵循当前工作区的权限模式；如果只希望检查而不修改文件，请显式指定只读权限：

```bash
wuu exec review --uncommitted --permission-mode read_only
```

## 版本和兼容入口

```bash
wuu version
wuu version --long
wuu version --json
```

旧脚本中如果仍使用 `wuu run`，它会转发到 `wuu exec`。新脚本建议直接使用 `wuu exec`，
因为 `run` 不支持旧版的 `--max-steps`、`--temperature` 和 `--system-prompt` 选项。

## 相关文档

- [用 `wuu exec` 做自动化](../automation/exec.md)：stdin、附件、JSONL、退出码和 CI 用法；
- [权限模式](permissions.md)：了解任务能否读写文件或执行命令；
- [配置参考](configuration.md)：配置文件和模型服务的详细说明。
