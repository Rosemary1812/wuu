/**
 * Design-system catalog of every chip variant the renderer can produce
 * for a turn-level user-facing outcome.
 *
 * Two sections:
 *   1. Gallery: each chip rendered in isolation with its kind / tone /
 *      description. Built by calling the same `userFacingErrorForMessage`
 *      / `userFacingErrorForMissingReply` / `ContextCompactionNotice`
 *      entry points the real turn pipeline uses, so what you see here
 *      is what the user sees in the conversation.
 *   2. In-Context: four mock `Turn` records rendered through the real
 *      `TurnView` component, so the chip is shown next to a user
 *      message bubble and an assistant turn body, at the real spacing.
 *
 * Gated behind the debug-controls switch in Settings. The switch is
 * itself only visible in development builds, so this catalog never
 * reaches production. See AGENTS.md "Desktop Debug Controls".
 */
import { X } from "lucide-react";
import { type JSX } from "react";
import type { Turn } from "../shared/protocol";
import {
  userFacingErrorForMessage,
  userFacingErrorForMissingReply,
  type UserFacingErrorDisplay,
} from "./UserFacingErrors";
import { ContextCompactionNotice, TurnNotice } from "./TurnNotice";
import { TurnView } from "./TurnView";

export function ChipGalleryPanel({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}): JSX.Element | null {
  if (!open) {
    return null;
  }
  return (
    <div
      className="chip-gallery-backdrop"
      role="presentation"
      onClick={onClose}
    >
      <div
        className="chip-gallery-panel"
        role="dialog"
        aria-label="Chip 图鉴"
        aria-modal="true"
        onClick={(event) => event.stopPropagation()}
      >
        <header className="chip-gallery-header">
          <div>
            <h2>Chip 图鉴</h2>
            <p>
              Turn-level 用户通知 chip 的设计系统图鉴。所有变体由
              <code>userFacingErrorForMessage</code> /
              <code>userFacingErrorForMissingReply</code> /
              <code>ContextCompactionNotice</code> 统一渲染,跟 conversation
              流里看到的一致。点击背景关闭。
            </p>
          </div>
          <button
            className="icon-button"
            type="button"
            aria-label="关闭"
            onClick={onClose}
          >
            <X className="icon" />
          </button>
        </header>
        <div className="chip-gallery-body">
          <ChipGalleryGallery />
          <ChipGalleryInContext />
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Gallery section: every chip variant in isolation
// ---------------------------------------------------------------------------

type GalleryEntry = {
  /** Short label shown next to the chip. */
  label: string;
  /** `kind · tone` badge. Drives the visual contract documentation. */
  kind: string;
  /** One-line description of when this chip fires. */
  description: string;
  /** Render the actual chip using the same entry points the turn pipeline uses. */
  render: () => JSX.Element;
};

const GALLERY_ENTRIES: GalleryEntry[] = [
  // -----------------------------------------------------------------------
  // Cancellation / soft outcomes
  // -----------------------------------------------------------------------
  {
    label: "已停止",
    kind: "cancelled · neutral",
    description: "用户中断,无已生成内容",
    render: () => <TurnNotice display={cancelledEmpty()} />,
  },
  {
    label: "回复已中断",
    kind: "cancelled · neutral",
    description: "用户中断,有已生成内容(已保留)",
    render: () => <TurnNotice display={cancelledWithOutput()} />,
  },
  {
    label: "无最终回答",
    kind: "missing_final_answer · warning",
    description: "完成但只有 commentary,无 final_answer",
    render: () => <TurnNotice display={userFacingErrorForMissingReply()} />,
  },

  // -----------------------------------------------------------------------
  // Context compaction
  // -----------------------------------------------------------------------
  {
    label: "正在自动压缩上下文",
    kind: "context_compacting · progress",
    description: "压缩进行中,带共享 live-gray sweep",
    render: () => <ContextCompactionNotice status="in_progress" />,
  },
  {
    label: "上下文已压缩",
    kind: "context_compacted · gray",
    description: "正常压缩完成",
    render: () => (
      <ContextCompactionNotice
        status="completed"
        text="✦ Compacted history: 18 → 5 messages (was ~12k tokens)"
      />
    ),
  },
  {
    label: "已续接上下文",
    kind: "context_inception · gray",
    description: "Inception 续接摘要已写入上下文",
    render: () => (
      <ContextCompactionNotice
        status="completed"
        reason="inception"
        text="✦ Inception rewrote history: 227 → 3 messages (was ~175k tokens)"
      />
    ),
  },
  {
    label: "已合并求助结果",
    kind: "context_helpme · gray",
    description: "HelpMe 恢复结果已合并进上下文",
    render: () => (
      <ContextCompactionNotice
        status="completed"
        reason="helpme"
        text="✦ HelpMe recovered and compacted history: 42 → 2 messages (was ~90k tokens)"
      />
    ),
  },
  {
    label: "上下文压缩失败",
    kind: "context_compaction_failed · gray",
    description: "自动压缩失败,保留原上下文",
    render: () => (
      <ContextCompactionNotice
        status="completed"
        text="Context compaction failed; continuing without compacting history."
      />
    ),
  },

  // -----------------------------------------------------------------------
  // Provider errors (error tone)
  // -----------------------------------------------------------------------
  {
    label: "context_length_exceeded",
    kind: "provider · error",
    description: "输入超过上下文窗口",
    render: () => (
      <TurnNotice
        display={userFacingErrorForMessage(
          "context_length_exceeded: Your input exceeds the context window",
          "turn",
        )}
      />
    ),
  },
  {
    label: "stream_closed_before_response.completed",
    kind: "provider · error",
    description: "Provider WS 流在 response.completed 前断开",
    render: () => (
      <TurnNotice
        display={userFacingErrorForMessage(
          "stream request failed: websocket stream closed before response.completed",
          "turn",
        )}
      />
    ),
  },
  {
    label: "connection reset",
    kind: "network · error",
    description: "TCP 连接被对端重置",
    render: () => (
      <TurnNotice
        display={userFacingErrorForMessage("connection reset by peer", "turn")}
      />
    ),
  },
  {
    label: "timeout",
    kind: "network · error",
    description: "请求超时 / deadline exceeded",
    render: () => (
      <TurnNotice
        display={userFacingErrorForMessage("deadline exceeded", "turn")}
      />
    ),
  },

  // -----------------------------------------------------------------------
  // Auth (auth tone)
  // -----------------------------------------------------------------------
  {
    label: "401 unauthorized",
    kind: "auth · auth",
    description: "Provider 凭据无效",
    render: () => (
      <TurnNotice
        display={userFacingErrorForMessage("401 unauthorized", "turn")}
      />
    ),
  },
  {
    label: "403 forbidden",
    kind: "auth · auth",
    description: "Provider 权限不足",
    render: () => (
      <TurnNotice
        display={userFacingErrorForMessage("403 forbidden", "turn")}
      />
    ),
  },

  // -----------------------------------------------------------------------
  // Tool / Internal (error tone)
  // -----------------------------------------------------------------------
  {
    label: "command failed",
    kind: "local · error",
    description: "本地命令执行失败(exit status)",
    render: () => (
      <TurnNotice
        display={userFacingErrorForMessage(
          "command failed: exit status 1: cat: /nonexistent: No such file or directory",
          "turn",
        )}
      />
    ),
  },
  {
    label: "panic: nil pointer",
    kind: "internal · error",
    description: "wuu 内部错误",
    render: () => (
      <TurnNotice
        display={userFacingErrorForMessage("panic: nil pointer", "turn")}
      />
    ),
  },
];

/**
 * The two `cancelled` displays are not produced by `userFacingErrorForMessage`
 * directly — `turnEventForTurn` overrides the title / detail based on whether
 * the turn had preserved output. Build them explicitly so the gallery
 * documents the exact strings the user sees.
 */
function cancelledEmpty(): UserFacingErrorDisplay {
  return {
    category: "cancelled",
    tone: "neutral",
    title: "已停止",
    detail: "这次请求已停止，没有生成回复内容。",
    recommendedActions: [],
  };
}

function cancelledWithOutput(): UserFacingErrorDisplay {
  return {
    category: "cancelled",
    tone: "neutral",
    title: "回复已中断",
    detail: "已保留已生成内容，可以继续发送消息。",
    recommendedActions: [],
  };
}

function ChipGalleryGallery(): JSX.Element {
  return (
    <section className="chip-gallery-section">
      <header>
        <h3>Gallery</h3>
        <p>
          按 <code>kind · tone</code> 分组,每行独立展示一个 chip。hover
          chip 看完整 detail。
        </p>
      </header>
      <ul className="chip-gallery-entries">
        {GALLERY_ENTRIES.map((entry, index) => (
          <li
            key={`${entry.kind}-${index}`}
            className="chip-gallery-entry"
            data-kind={entry.kind}
          >
            <div className="chip-gallery-entry-chip">{entry.render()}</div>
            <div className="chip-gallery-entry-meta">
              <strong className="chip-gallery-entry-label">{entry.label}</strong>
              <code className="chip-gallery-entry-kind">{entry.kind}</code>
              <span className="chip-gallery-entry-description">
                {entry.description}
              </span>
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}

// ---------------------------------------------------------------------------
// In-Context section: chips as they appear in the conversation stream
// ---------------------------------------------------------------------------

type ContextTurn = {
  /** Section heading shown above the mock turn. */
  heading: string;
  /** The mock turn rendered through the real `TurnView` component. */
  turn: Turn;
};

/**
 * Four representative scenarios. Each one constructs a `Turn` with the
 * minimum items needed to surface the target chip via the real
 * `turnEventForTurn` / `turnEventForItem` pipeline.
 */
const CONTEXT_TURNS: ContextTurn[] = [
  {
    heading: "用户中断,无已生成内容",
    turn: {
      id: "demo-cancelled-empty",
      // No assistant items on purpose — `turnHasAssistantOutput(turn)`
      // must return false so the real `turnEventForTurn` pipeline
      // picks the `已停止` title (vs `回复已中断` for the partial-
      // output case below). The user message bubble still renders
      // from `userItems` in TurnView, so the in-context view shows
      // a question followed by the chip with no assistant body.
      items: [userMessage("帮我看一下 src/ 目录结构")],
      items_view: "full",
      status: "interrupted",
    },
  },
  {
    heading: "用户中断,有已生成内容(已保留)",
    turn: {
      id: "demo-cancelled-partial",
      items: [
        userMessage("写一个解析 JSON 的函数"),
        commentary("好的,先看一下需求..."),
        finalAnswer(
          "下面是一个支持嵌套对象和数组的 JSON 解析器:\n\n```js\nfunction parseJSON(input) {\n  return JSON.parse(input);\n}\n```",
        ),
      ],
      items_view: "full",
      status: "interrupted",
    },
  },
  {
    heading: "完成但只有 commentary,无最终回答",
    turn: {
      id: "demo-missing-reply",
      items: [
        userMessage("总结一下刚才的讨论"),
        commentary("我先回顾一下..."),
        commentary("看起来有几个关键点..."),
        commentary("让我再看看第三个文件..."),
      ],
      items_view: "full",
      status: "completed",
    },
  },
  {
    heading: "Turn 中含 context_compaction item",
    turn: {
      id: "demo-compact",
      items: [
        userMessage("继续之前的工作"),
        {
          id: "compact-1",
          type: "context_compaction",
          status: "completed",
          text: "✦ Compacted history: 18 → 5 messages (was ~12k tokens)",
        },
        finalAnswer("好的,继续工作。"),
      ],
      items_view: "full",
      status: "completed",
    },
  },
  {
    heading: "Provider 错误(上下文超限) + 推荐操作",
    turn: {
      id: "demo-failed",
      items: [userMessage("分析这个大文件")],
      items_view: "full",
      status: "failed",
      error: {
        message: "context_length_exceeded: Your input exceeds the context window",
        code: "context_length_exceeded",
        category: "provider",
      },
    },
  },
];

function userMessage(text: string): Turn["items"][number] {
  return {
    id: `u-${text.slice(0, 8)}`,
    type: "user_message",
    role: "user",
    status: "completed",
    text,
  };
}

function commentary(text: string): Turn["items"][number] {
  return {
    id: `c-${text.slice(0, 8)}`,
    type: "agent_message",
    role: "assistant",
    phase: "commentary",
    status: "completed",
    text,
  };
}

function finalAnswer(text: string): Turn["items"][number] {
  return {
    id: `f-${text.slice(0, 8)}`,
    type: "agent_message",
    role: "assistant",
    phase: "final_answer",
    status: "completed",
    text,
  };
}

function ChipGalleryInContext(): JSX.Element {
  return (
    <section className="chip-gallery-section">
      <header>
        <h3>In-Context</h3>
        <p>
          Mock 出来的 turn,走真实 <code>TurnView</code> 渲染,展示 chip
          在 user 消息下方 / assistant turn 后的实际位置和间距。
        </p>
      </header>
      <ul className="chip-gallery-context">
        {CONTEXT_TURNS.map((entry) => (
          <li
            key={entry.turn.id}
            className="chip-gallery-context-item"
            data-heading={entry.heading}
          >
            <h4>{entry.heading}</h4>
            <div className="chip-gallery-context-frame">
              <TurnView
                turn={entry.turn}
                onStreamFrame={() => {}}
                onNoticeAction={() => {}}
              />
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}
