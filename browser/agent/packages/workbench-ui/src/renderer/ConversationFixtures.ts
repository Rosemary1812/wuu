import type { InitializeResult, Thread } from "../shared/protocol";

export type ConversationFixtureKind = "long" | "rich" | "running" | "compact" | "plan";

export function createAgentTreeDemo(cwd: string, initialized?: InitializeResult): { parent: Thread; children: Thread[] } {
  const base = Date.now();
  const at = (offsetMs: number): string => new Date(base + offsetMs).toISOString();
  const parentID = `demo-agent-tree-${base}`;
  const inspectID = `${parentID}-inspect-sidebar`;
  const resumeID = `${parentID}-resume-child`;
  const provider = initialized?.provider ?? "demo-provider";
  const model = initialized?.model ?? "demo-model";
  const parentTurnID = `${parentID}-turn-0001`;
  const inspectTurnID = `${inspectID}-turn-0001`;
  const resumeTurnID = `${resumeID}-turn-0001`;

  const parent: Thread = {
    id: parentID,
    preview: "模拟：父 agent 分发子任务",
    model_provider: provider,
    model,
    cwd,
    status: "idle",
    created_at: at(0),
    updated_at: at(4000),
    turns: [
      {
        id: parentTurnID,
        items_view: "full",
        status: "completed",
        items: [
          {
            id: `${parentTurnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: "把左侧 session 树改成可以展示父 agent 和子 agent 的层级。"
          },
          {
            id: `${parentTurnID}-item-2`,
            type: "collab_agent_tool_call",
            status: "completed",
            name: "spawn_agent",
            arguments: `{"task_name":"检查左侧树","message":"验证子 agent 行的视觉和状态"}`,
            result: `{"agent_id":"${inspectID}","agent_path":"/root/inspect-sidebar","status":"completed"}`
          },
          {
            id: `${parentTurnID}-item-3`,
            type: "collab_agent_tool_call",
            status: "completed",
            name: "spawn_agent",
            arguments: `{"task_name":"子会话恢复","message":"确认点击子 agent 后能正常查看会话"}`,
            result: `{"agent_id":"${resumeID}","agent_path":"/root/resume-child","status":"running"}`
          },
          {
            id: `${parentTurnID}-item-4`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text: "已分发两个子 agent。左侧只展示直属子任务；更深层级会折叠成数量提示。"
          }
        ]
      }
    ],
    child_agents: [
      {
        id: inspectID,
        type: "worker",
        task_name: "检查左侧树",
        agent_path: "/root/inspect-sidebar",
        parent_id: parentID,
        description: "验证子 agent 行的视觉和状态",
        status: "completed",
        nested_count: 1,
        nested_running_count: 0,
        started_at: at(1000),
        completed_at: at(2500)
      },
      {
        id: resumeID,
        type: "worker",
        task_name: "子会话恢复",
        agent_path: "/root/resume-child",
        parent_id: parentID,
        description: "确认点击子 agent 后能正常查看会话",
        status: "running",
        nested_count: 1,
        nested_running_count: 1,
        started_at: at(1800)
      }
    ]
  };

  const children: Thread[] = [
    {
      id: inspectID,
      parent_id: parentID,
      agent_path: "/root/inspect-sidebar",
      preview: "检查左侧树",
      model_provider: provider,
      model,
      cwd,
      status: "idle",
      read_only: true,
      created_at: at(1000),
      updated_at: at(2500),
      turns: [
        {
          id: inspectTurnID,
          items_view: "full",
          status: "completed",
          items: [
            {
              id: `${inspectTurnID}-item-1`,
              type: "user_message",
              status: "completed",
              role: "user",
              text: "检查左侧 session 树中子 agent 的缩进、状态和折叠数量。"
            },
            {
              id: `${inspectTurnID}-item-2`,
              type: "agent_message",
              status: "completed",
              role: "assistant",
              text: "子 agent 显示在父会话下方，保留一层缩进；更深层级只显示数量，不再展开。"
            }
          ]
        }
      ]
    },
    {
      id: resumeID,
      parent_id: parentID,
      agent_path: "/root/resume-child",
      preview: "子会话恢复",
      model_provider: provider,
      model,
      cwd,
      status: "idle",
      read_only: true,
      created_at: at(1800),
      updated_at: at(4200),
      turns: [
        {
          id: resumeTurnID,
          items_view: "full",
          status: "completed",
          items: [
            {
              id: `${resumeTurnID}-item-1`,
              type: "user_message",
              status: "completed",
              role: "user",
              text: "验证点击左侧子 agent 后，主区域可以正常展示这个子会话。"
            },
            {
              id: `${resumeTurnID}-item-2`,
              type: "agent_message",
              status: "completed",
              role: "assistant",
              text: "这个视图是只读的子 agent session。左侧仍保留父子关系，主区域展示子 agent 的对话内容。"
            }
          ]
        }
      ]
    }
  ];

  return { parent, children };
}

export function createConversationFixture(kind: ConversationFixtureKind, cwd: string, initialized?: InitializeResult): Thread {
  switch (kind) {
    case "rich":
      return createRichContentFixture(cwd, initialized);
    case "running":
      return createRunningFixture(cwd, initialized);
    case "compact":
      return createContextCompactionFixture(cwd, initialized);
    case "plan":
      return createPlanPanelFixture(cwd, initialized);
    default:
      return createLongReadingFixture(cwd, initialized);
  }
}

function fixtureRuntime(initialized?: InitializeResult): { provider: string; model: string } {
  return {
    provider: initialized?.provider ?? "demo-provider",
    model: initialized?.model ?? "demo-model"
  };
}

function createLongReadingFixture(cwd: string, initialized?: InitializeResult): Thread {
  const base = Date.now();
  const at = (offsetMs: number): string => new Date(base + offsetMs).toISOString();
  const { provider, model } = fixtureRuntime(initialized);
  const threadID = `demo-long-reading-${base}`;
  const firstTurnID = `${threadID}-turn-0001`;
  const secondTurnID = `${threadID}-turn-0002`;
  const thirdTurnID = `${threadID}-turn-0003`;

  return {
    id: threadID,
    preview: "模拟：长阅读宽度",
    model_provider: provider,
    model,
    cwd,
    status: "idle",
    created_at: at(-18_000),
    updated_at: at(4000),
    turns: [
      {
        id: firstTurnID,
        items_view: "full",
        status: "completed",
        started_at: at(-18_000),
        completed_at: at(-12_000),
        duration_ms: 6000,
        items: [
          {
            id: `${firstTurnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: "这条假消息用来检查大屏上用户气泡、助手回复和段落行长是否在合适的阅读范围内。"
          },
          {
            id: `${firstTurnID}-item-2`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text:
              "阅读列现在应该像一条稳定的正文栏，而不是随着窗口一直变宽。对于中文对话，过长的行会让眼睛在换行时很难回到下一行开头；对于代码解释和产品判断，过宽也会让段落看起来像日志输出，而不是可读的对话。\n\n这个样例故意放入较长段落，方便检查窗口拉宽时正文是否仍然保持在舒适区域。理想状态是：主面板依旧宽敞，左右留白增加，但真正需要连续阅读的文字不会无限拉伸。\n\n如果这个回复在大屏上仍然显得太宽，下一步应该调小 `--conversation-readable-width`，而不是压缩整个应用主区域。"
          }
        ]
      },
      {
        id: secondTurnID,
        items_view: "full",
        status: "completed",
        started_at: at(-10_000),
        completed_at: at(-6000),
        duration_ms: 4000,
        items: [
          {
            id: `${secondTurnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: "再给一条短一点的追问，看看上下文连续时的节奏。"
          },
          {
            id: `${secondTurnID}-item-2`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text:
              "短回复也应该自然贴在同一条阅读轴线上，不应该因为内容短就显得漂在页面中央。\n\n- 用户消息靠右，但不要过宽。\n- 助手消息靠左，占据稳定阅读列。\n- 每轮之间的间距要足够分辨上下文，但不能像章节分隔一样夸张。"
          }
        ]
      },
      {
        id: thirdTurnID,
        items_view: "full",
        status: "completed",
        started_at: at(-5000),
        completed_at: at(0),
        duration_ms: 5000,
        items: [
          {
            id: `${thirdTurnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: "最后放一个更长的结论段，用来检查滚动到底部时 composer 和正文列是否对齐。"
          },
          {
            id: `${thirdTurnID}-item-2`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text:
              "底部输入框可以略宽于正文，因为输入是编辑动作，用户需要一点横向空间。但对话历史是阅读动作，应该优先照顾视线移动和段落扫描。这个区别决定了我们不应该简单让所有东西同宽，也不应该把主窗口空间全部让给文本。"
          }
        ]
      }
    ]
  };
}

function createRichContentFixture(cwd: string, initialized?: InitializeResult): Thread {
  const base = Date.now();
  const at = (offsetMs: number): string => new Date(base + offsetMs).toISOString();
  const { provider, model } = fixtureRuntime(initialized);
  const threadID = `demo-rich-content-${base}`;
  const turnID = `${threadID}-turn-0001`;

  return {
    id: threadID,
    preview: "模拟：富内容消息",
    model_provider: provider,
    model,
    cwd,
    status: "idle",
    created_at: at(-9000),
    updated_at: at(1000),
    turns: [
      {
        id: turnID,
        items_view: "full",
        status: "completed",
        started_at: at(-9000),
        completed_at: at(1000),
        duration_ms: 10_000,
        items: [
          {
            id: `${turnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: "生成一个覆盖 Markdown、工具卡、代码块和系统提示的假会话。"
          },
          {
            id: `${turnID}-item-2`,
            type: "reasoning",
            status: "completed",
            text: "先检查信息层级，再确认长文本、列表、表格和代码块在同一阅读列内不会撑破布局。"
          },
          {
            id: `${turnID}-item-3`,
            type: "tool_call",
            status: "completed",
            name: "rg",
            arguments: `{"pattern":"conversation-width","path":"browser/agent/packages/workbench-ui/src/renderer/styles.css"}`,
            result: "browser/agent/packages/workbench-ui/src/renderer/styles.css:3244:.conversation-width"
          },
          {
            id: `${turnID}-item-4`,
            type: "tool_call",
            status: "completed",
            name: "apply_patch",
            arguments: `{"file":"browser/agent/packages/workbench-ui/src/renderer/styles.css","summary":"constrain readable width"}`,
            result: "Patch applied."
          },
          {
            id: `${turnID}-item-5`,
            type: "context_compaction",
            status: "completed",
            text: "上下文已压缩：保留布局目标、调试入口和验证结果。"
          },
          {
            id: `${turnID}-item-6`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text:
              "## 视觉检查清单\n\n| 区域 | 预期 |\n| --- | --- |\n| 正文段落 | 行长稳定，窗口变宽只增加留白 |\n| 用户气泡 | 靠右显示，不能横跨整列 |\n| 工具卡片 | 保持在阅读列内，不挤压复制按钮 |\n| 代码块 | 可以横向或换行展示，不能撑破页面 |\n\n> 这个引用块用来检查左边界、缩进和正文宽度是否协调。\n\n```ts\nconst readableWidth = \"760px\";\nconst goal = \"keep conversation text comfortable on wide screens\";\n```\n\n这个样例不代表真实历史，也不会写入后端。它只在开发模式下用于看界面效果。"
          },
          {
            id: `${turnID}-item-7`,
            type: "error",
            status: "failed",
            error: "模拟错误：用于检查错误块在阅读列内的视觉效果。"
          }
        ]
      }
    ]
  };
}

function createPlanPanelFixture(cwd: string, initialized?: InitializeResult): Thread {
  const base = Date.now();
  const at = (offsetMs: number): string => new Date(base + offsetMs).toISOString();
  const { provider, model } = fixtureRuntime(initialized);
  const threadID = `demo-plan-panel-${base}`;
  const turnID = `${threadID}-turn-0001`;

  return {
    id: threadID,
    preview: "模拟：信息面板进度",
    model_provider: provider,
    model,
    cwd,
    status: "idle",
    created_at: at(-6000),
    updated_at: at(1000),
    turns: [
      {
        id: turnID,
        items_view: "full",
        status: "completed",
        started_at: at(-6000),
        completed_at: at(1000),
        duration_ms: 7000,
        items: [
          {
            id: `${turnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: "看看信息面板里的 update_plan 进度样式。"
          },
          {
            id: `${turnID}-item-2`,
            type: "tool_call",
            status: "completed",
            name: "update_plan",
            arguments: JSON.stringify({
              plan: [
                { step: "定位信息面板和 plan_update 数据流", status: "completed" },
                { step: "设计并接入任务计划 UI", status: "completed" },
                { step: "验证桌面构建和类型检查", status: "in_progress" },
                { step: "原子提交改动", status: "pending" }
              ]
            }),
            result: `{"status":"updated"}`
          },
          {
            id: `${turnID}-item-3`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text: "已注入一个本地调试计划。打开右上角信息面板可以看到顶部的进度清单。"
          }
        ]
      }
    ]
  };
}

function createContextCompactionFixture(cwd: string, initialized?: InitializeResult): Thread {
  const base = Date.now();
  const at = (offsetMs: number): string => new Date(base + offsetMs).toISOString();
  const { provider, model } = fixtureRuntime(initialized);
  const threadID = `demo-context-compaction-${base}`;
  const beforeTurnID = `${threadID}-turn-0001`;
  const compactTurnID = `${threadID}-turn-0002`;

  return {
    id: threadID,
    preview: "模拟：上下文压缩",
    model_provider: provider,
    model,
    cwd,
    status: "idle",
    created_at: at(-32_000),
    updated_at: at(2000),
    turns: [
      {
        id: beforeTurnID,
        items_view: "full",
        status: "completed",
        started_at: at(-32_000),
        completed_at: at(-24_000),
        duration_ms: 8000,
        items: [
          {
            id: `${beforeTurnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text:
              "假设这个会话已经很长，模型上下文快满了。继续执行之前，wuu 会先把较早的历史压缩成摘要。"
          },
          {
            id: `${beforeTurnID}-item-2`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text:
              "这个调试样例只用来观察界面效果，不会真的触发模型压缩，也不会写入后端会话。\n\n真实流程里，压缩完成后 GUI 会收到一个 `context_compaction` item，并把它显示成一条系统线。"
          }
        ]
      },
      {
        id: compactTurnID,
        items_view: "full",
        status: "completed",
        started_at: at(-20_000),
        completed_at: at(2000),
        duration_ms: 22_000,
        items: [
          {
            id: `${compactTurnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: "继续刚才的任务。"
          },
          {
            id: `${compactTurnID}-item-2`,
            type: "context_compaction",
            status: "completed",
            text: "✦ Compacted history: 18 → 5 messages (was ~12k tokens)"
          },
          {
            id: `${compactTurnID}-item-3`,
            type: "agent_message",
            status: "completed",
            role: "assistant",
            text:
              "压缩完成后，对话会继续正常显示后续回复。用户能看到的主要变化就是中间这条灰色系统提示线；调试面板里也会把它标成“上下文压缩”。"
          }
        ]
      }
    ]
  };
}

function createRunningFixture(cwd: string, initialized?: InitializeResult): Thread {
  const base = Date.now();
  const at = (offsetMs: number): string => new Date(base + offsetMs).toISOString();
  const { provider, model } = fixtureRuntime(initialized);
  const threadID = `demo-running-${base}`;
  const turnID = `${threadID}-turn-0001`;

  return {
    id: threadID,
    preview: "模拟：运行中状态",
    model_provider: provider,
    model,
    cwd,
    status: "in_progress",
    created_at: at(-45_000),
    updated_at: at(0),
    turns: [
      {
        id: turnID,
        items_view: "full",
        status: "in_progress",
        started_at: at(-45_000),
        items: [
          {
            id: `${turnID}-item-1`,
            type: "user_message",
            status: "completed",
            role: "user",
            text: "模拟一个还在运行的回复，看看等待状态、推理块和半截回答的排版。"
          },
          {
            id: `${turnID}-item-2`,
            type: "reasoning",
            status: "in_progress",
            text: "正在判断哪些内容属于阅读列，哪些控件应该保持在操作区。"
          },
          {
            id: `${turnID}-item-3`,
            type: "tool_call",
            status: "in_progress",
            name: "npm run typecheck",
            arguments: `{"cwd":"desktop"}`,
            result: ""
          },
          {
            id: `${turnID}-item-4`,
            type: "agent_message",
            status: "in_progress",
            role: "assistant",
            text:
              "我正在生成回复。这个假状态不会连接真实模型，也不会自动结束；它只用于检查运行中 turn 的等待文案、动效和底部输入区遮挡关系。"
          }
        ]
      }
    ]
  };
}
