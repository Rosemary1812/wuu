// Chat-row derivation, mirrored from desktop AppState.chatMessagesFromTurns
// (chat-style-threads-design.md §2): only user messages, envelope meta rows,
// tool-posted participant messages, task cards, and workspace-focus divider
// rows are chat messages. The agent's working transcript (reasoning, tool
// calls, agent_message streams) never reaches the UI.

import type { ThreadItem, Turn } from "@wuu/protocol";

import { isAgentHandoffText } from "./handoff";

export type ChatRow =
  | { kind: "focus"; id: string; turnID: string; item: ThreadItem }
  | { kind: "user"; id: string; turnID: string; item: ThreadItem }
  | { kind: "envelope"; id: string; turnID: string; items: ThreadItem[] }
  | { kind: "participant"; id: string; turnID: string; item: ThreadItem }
  | { kind: "task"; id: string; turnID: string; item: ThreadItem };

export function chatRowsFromTurns(
  turns: ReadonlyArray<Pick<Turn, "id" | "items">>,
): ChatRow[] {
  const rows: ChatRow[] = [];
  for (const turn of turns) {
    for (const item of turn.items ?? []) {
      const id = `${turn.id}:${item.id}`;
      // focus_meta rides on *some* item in the stream and wins regardless of
      // the item's type — mirror the desktop's ordering exactly.
      if (item.focus_meta) {
        rows.push({ kind: "focus", id, turnID: turn.id, item });
      } else if (item.type === "user_message") {
        if (isAgentHandoffText(item.text)) {
          continue; // inter-agent machinery, never a chat bubble
        }
        if (item.envelope_meta && item.envelope_meta.length > 0) {
          const previous = rows[rows.length - 1];
          if (previous?.kind === "envelope") {
            previous.items.push(item);
          } else {
            rows.push({ kind: "envelope", id, turnID: turn.id, items: [item] });
          }
        } else {
          rows.push({ kind: "user", id, turnID: turn.id, item });
        }
      } else if (item.type === "participant_message") {
        rows.push({ kind: "participant", id, turnID: turn.id, item });
      } else if (item.type === "task_card") {
        rows.push({ kind: "task", id, turnID: turn.id, item });
      }
    }
  }
  return rows;
}

/** Divider label for a focus row: ⌂ 个人 / ⬒ 工作区名 / ⬒ 全部工作区. */
export function focusRowLabel(item: ThreadItem): string {
  const meta = item.focus_meta;
  if (!meta) return "";
  if (meta.kind === "home") return "⌂ 个人";
  if (meta.kind === "workspace") return `⬒ ${meta.name?.trim() || "工作区"}`;
  return "⬒ 全部工作区";
}

/** Divider label for an envelope row (mirrors desktop EnvelopeNotice copy). */
export function envelopeRowLabel(items: ReadonlyArray<ThreadItem>): string {
  const titles = new Set<string>();
  let count = 0;
  for (const item of items) {
    for (const record of item.envelope_meta ?? []) {
      count++;
      const title = record.source_thread_title?.trim();
      if (title) titles.add(title);
    }
  }
  if (count === 0) count = items.length;
  const source =
    titles.size === 0 ? "其他会话" : titles.size === 1 ? `「${[...titles][0]}」` : "多个会话";
  return count > 1 ? `收到来自${source}的 ${count} 条消息` : `收到来自${source}的消息`;
}

/** 拒答折叠行文案:{名字} 认为无需回应:{text}。 */
export function isDeclineRow(item: ThreadItem): boolean {
  return item.type === "participant_message" && item.post_kind === "decline";
}

/** Task-card status labels (mirrors desktop TaskCardItem). */
export function taskStatusLabel(status: string): string {
  switch (status) {
    case "pending":
      return "待开始";
    case "queued":
      return "排队中";
    case "in_progress":
      return "进行中";
    case "reporting":
      return "待报告";
    case "completed":
      return "已完成";
    case "failed":
      return "失败";
    case "cancelled":
      return "已取消";
    default:
      return "未知";
  }
}
