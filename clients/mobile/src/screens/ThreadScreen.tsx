// The chat view. Renders the whitelist rows only (user / participant /
// task / envelope / focus) — the agents' working transcript never reaches
// this screen. The user bubble keeps the desktop's tucked-corner signature
// (12/12/3/12); everything else stays plain.

import { useMemo, useRef, useState } from "react";
import {
  FlatList,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import type { MessageMarkWire, ParticipantSummary, Thread, ThreadItem } from "@wuu/protocol";

import {
  ChatRow,
  chatRowsFromTurns,
  envelopeRowLabel,
  focusRowLabel,
  isDeclineRow,
  taskStatusLabel,
} from "../lib/chatModel";
import { isDMThread, isThreadRunning, threadDisplayTitle } from "../lib/threads";
import { applyMention, mentionCandidates } from "../lib/mentions";
import type { AppSnapshot, PendingSend } from "../lib/store";
import { usePalette } from "../theme";
import { Avatar } from "../components/Avatar";
import { ConnectionBanner } from "../components/ConnectionBanner";
import { MemberStack } from "../components/MemberStack";
import { ReactionChips, ReactionPicker } from "../components/ReactionBar";

type ListEntry =
  | { key: string; kind: "row"; row: ChatRow }
  | { key: string; kind: "pending"; send: PendingSend }
  | { key: string; kind: "running" };

export function ThreadScreen({
  snapshot,
  threadId,
  onBack,
  onSend,
  onInterrupt,
  onReact,
}: {
  snapshot: AppSnapshot;
  threadId: string;
  onBack: () => void;
  onSend: (thread: Thread, text: string) => Promise<void>;
  onInterrupt: (threadId: string) => void;
  onReact: (threadId: string, seq: number, reaction: string) => void;
}): React.JSX.Element {
  const palette = usePalette();
  const [draft, setDraft] = useState("");
  const [sendError, setSendError] = useState("");
  const [pickerSeq, setPickerSeq] = useState<number | null>(null);
  const listRef = useRef<FlatList<ListEntry>>(null);

  const thread = snapshot.threads.find((t) => t.id === threadId);
  const running = thread ? isThreadRunning(thread) : false;
  const isDM = thread ? isDMThread(thread) : false;
  const members = thread?.members ?? [];
  const marks = snapshot.marks[threadId] ?? [];

  const marksBySeq = useMemo(() => {
    const map = new Map<number, MessageMarkWire[]>();
    for (const mark of marks) {
      const list = map.get(mark.seq) ?? [];
      list.push(mark);
      map.set(mark.seq, list);
    }
    return map;
  }, [marks]);

  const entries = useMemo<ListEntry[]>(() => {
    if (!thread) return [];
    const rows = chatRowsFromTurns(thread.turns ?? []).map<ListEntry>((row) => ({
      key: row.id,
      kind: "row",
      row,
    }));
    for (const send of snapshot.pending.filter((p) => p.threadId === threadId)) {
      rows.push({ key: `pending:${send.clientId}`, kind: "pending", send });
    }
    if (running) rows.push({ key: "running", kind: "running" });
    return rows.reverse(); // inverted list renders bottom-up
  }, [thread, snapshot.pending, threadId, running]);

  const mentionMatches = useMemo(
    () => (thread?.group ? mentionCandidates(draft, members) : []),
    [draft, members, thread?.group],
  );

  const send = () => {
    if (!thread) return;
    const text = draft.trim();
    if (text === "") return;
    setDraft("");
    setSendError("");
    onSend(thread, text).catch((err: unknown) => {
      setSendError(err instanceof Error ? err.message : String(err));
      setDraft(text); // restore the draft so nothing is lost
    });
  };

  if (!thread) {
    return (
      <View style={[styles.page, styles.missing, { backgroundColor: palette.paper }]}>
        <Text style={{ color: palette.inkMuted }}>会话不存在</Text>
        <Pressable onPress={onBack}>
          <Text style={{ color: palette.accent, marginTop: 12 }}>返回</Text>
        </Pressable>
      </View>
    );
  }

  return (
    <KeyboardAvoidingView
      style={[styles.page, { backgroundColor: palette.paper }]}
      behavior={Platform.OS === "ios" ? "padding" : undefined}
    >
      <View style={[styles.header, { borderColor: palette.hairline }]}>
        <Pressable onPress={onBack} hitSlop={12} style={styles.backButton}>
          <Text style={[styles.backGlyph, { color: palette.ink }]}>‹</Text>
        </Pressable>
        <View style={styles.headerTitleCell}>
          <Text style={[styles.headerTitle, { color: palette.inkStrong }]} numberOfLines={1}>
            {threadDisplayTitle(thread)}
          </Text>
          {thread.group ? (
            <Text style={[styles.headerMeta, { color: palette.inkMuted }]}>
              {members.length} 位成员
            </Text>
          ) : null}
        </View>
        {thread.group ? <MemberStack members={members} /> : null}
      </View>
      <ConnectionBanner phase={snapshot.phase} syncError={snapshot.syncError} />

      <FlatList
        ref={listRef}
        inverted
        data={entries}
        keyExtractor={(entry) => entry.key}
        contentContainerStyle={styles.listContent}
        renderItem={({ item }) => (
          <Entry
            entry={item}
            isDM={isDM}
            readerCount={isDM ? 1 : members.length}
            marksBySeq={marksBySeq}
            onLongPressMessage={(seq) => setPickerSeq(seq)}
          />
        )}
      />

      {sendError ? (
        <Text style={[styles.sendError, { color: palette.danger }]}>{sendError}</Text>
      ) : null}

      {mentionMatches.length > 0 ? (
        <View style={[styles.mentionSheet, { backgroundColor: palette.paper, borderColor: palette.hairline }]}>
          {mentionMatches.slice(0, 5).map((candidate) => (
            <Pressable
              key={candidate.id}
              style={styles.mentionRow}
              onPress={() => setDraft((value) => applyMention(value, candidate.name))}
            >
              <Text style={[styles.mentionName, { color: palette.ink }]}>{candidate.name}</Text>
              <Text style={[styles.mentionHint, { color: palette.inkFaint }]}>@{candidate.name}</Text>
            </Pressable>
          ))}
        </View>
      ) : null}

      <View style={[styles.composer, { borderColor: palette.hairline }]}>
        <TextInput
          style={[
            styles.input,
            { backgroundColor: palette.overlay4, color: palette.ink, borderColor: palette.hairline },
          ]}
          value={draft}
          onChangeText={setDraft}
          placeholder={isDM ? `有事直接跟 ${threadDisplayTitle(thread)} 说` : "有事直接在群里喊"}
          placeholderTextColor={palette.inkFaint}
          multiline
        />
        {running ? (
          <Pressable
            style={[styles.sendButton, { backgroundColor: palette.danger }]}
            onPress={() => onInterrupt(thread.id)}
            accessibilityLabel="停止"
          >
            <View style={styles.stopGlyph} />
          </Pressable>
        ) : (
          <Pressable
            style={[
              styles.sendButton,
              { backgroundColor: draft.trim() === "" ? palette.inkFaint : palette.accent },
            ]}
            onPress={send}
            disabled={draft.trim() === ""}
            accessibilityLabel="发送"
          >
            <Text style={styles.sendGlyph}>↑</Text>
          </Pressable>
        )}
      </View>

      <ReactionPicker
        visible={pickerSeq !== null}
        onClose={() => setPickerSeq(null)}
        onPick={(key) => {
          if (pickerSeq !== null) onReact(thread.id, pickerSeq, key);
        }}
      />
    </KeyboardAvoidingView>
  );
}

function Entry({
  entry,
  isDM,
  readerCount,
  marksBySeq,
  onLongPressMessage,
}: {
  entry: ListEntry;
  isDM: boolean;
  readerCount: number;
  marksBySeq: Map<number, MessageMarkWire[]>;
  onLongPressMessage: (seq: number) => void;
}): React.JSX.Element | null {
  const palette = usePalette();

  if (entry.kind === "pending") {
    return (
      <View style={styles.userRow}>
        <View style={[styles.userBubble, styles.pendingBubble, { backgroundColor: palette.userBubble }]}>
          <Text style={[styles.bubbleText, { color: palette.ink }]}>{entry.send.text}</Text>
        </View>
        <Text style={[styles.pendingHint, { color: palette.inkMuted }]}>发送中…</Text>
      </View>
    );
  }

  if (entry.kind === "running") {
    return (
      <View style={styles.runningRow}>
        <Text style={[styles.runningText, { color: palette.inkMuted }]}>正在回复…</Text>
      </View>
    );
  }

  const row = entry.row;
  switch (row.kind) {
    case "focus":
      return <Divider label={focusRowLabel(row.item)} />;
    case "envelope":
      return <Divider label={envelopeRowLabel(row.items)} />;
    case "user": {
      const seq = row.item.seq;
      const seqMarks = seq !== undefined ? (marksBySeq.get(seq) ?? []) : [];
      const seen = seqMarks.filter((m) => m.kind === "seen" && m.status === "completed").length;
      return (
        <View style={styles.userRow}>
          <Pressable
            style={[styles.userBubble, { backgroundColor: palette.userBubble }]}
            onLongPress={seq !== undefined ? () => onLongPressMessage(seq) : undefined}
            delayLongPress={280}
          >
            <Text style={[styles.bubbleText, { color: palette.ink }]} selectable>
              {row.item.text ?? ""}
            </Text>
          </Pressable>
          <ReactionChips marks={seqMarks} />
          {!isDM && seq !== undefined && readerCount > 0 ? (
            <Text style={[styles.receipt, { color: palette.inkFaint }]}>
              已读 {Math.min(seen, readerCount)}/{readerCount}
            </Text>
          ) : null}
        </View>
      );
    }
    case "participant": {
      if (isDeclineRow(row.item)) {
        const reason = row.item.text?.trim();
        return (
          <Text style={[styles.declineLine, { color: palette.inkMuted }]}>
            {row.item.participant?.name ?? "对方"} 认为无需回应
            {reason ? `：${reason}` : ""}
          </Text>
        );
      }
      const participant: ParticipantSummary | undefined = row.item.participant;
      const seq = row.item.seq;
      const seqMarks = seq !== undefined ? (marksBySeq.get(seq) ?? []) : [];
      return (
        <View style={styles.agentRow}>
          <Avatar
            id={participant?.id ?? row.item.agent_id}
            name={participant?.name}
            kind={participant?.kind ?? "resident"}
            imageUri={participant?.avatar_image}
            size={28}
          />
          <View style={styles.agentBody}>
            {participant?.name ? (
              <Text style={[styles.senderName, { color: palette.inkMuted }]}>{participant.name}</Text>
            ) : null}
            <Pressable
              style={[styles.agentBubble, { backgroundColor: palette.overlay4 }]}
              onLongPress={seq !== undefined ? () => onLongPressMessage(seq) : undefined}
              delayLongPress={280}
            >
              <Text style={[styles.bubbleText, { color: palette.ink }]} selectable>
                {row.item.text ?? ""}
              </Text>
            </Pressable>
            <ReactionChips marks={seqMarks} />
          </View>
        </View>
      );
    }
    case "task":
      return <TaskRow item={row.item} />;
    default:
      return null;
  }
}

function Divider({ label }: { label: string }): React.JSX.Element {
  const palette = usePalette();
  return (
    <View style={styles.divider}>
      <View style={[styles.dividerLine, { backgroundColor: palette.overlay8 }]} />
      <Text style={[styles.dividerLabel, { color: palette.inkFaint }]}>{label}</Text>
      <View style={[styles.dividerLine, { backgroundColor: palette.overlay8 }]} />
    </View>
  );
}

function TaskRow({ item }: { item: ThreadItem }): React.JSX.Element {
  const palette = usePalette();
  const task = item.task;
  const replies = task?.reply_count ?? 0;
  return (
    <View style={[styles.taskCard, { borderColor: palette.hairline, backgroundColor: palette.overlay4 }]}>
      <View style={styles.taskTop}>
        <Text style={[styles.taskName, { color: palette.ink }]} numberOfLines={1}>
          {task?.name?.trim() || "任务"}
        </Text>
        <Text style={[styles.taskStatus, { color: palette.inkMuted }]}>
          {taskStatusLabel(task?.status ?? "")}
        </Text>
      </View>
      {task?.description ? (
        <Text style={[styles.taskDescription, { color: palette.inkSoft }]} numberOfLines={2}>
          {task.description}
        </Text>
      ) : null}
      {replies > 0 ? (
        <Text style={[styles.taskReplies, { color: palette.inkFaint }]}>
          {replies > 99 ? "99+" : replies} 条回复
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  page: { flex: 1 },
  missing: { alignItems: "center", justifyContent: "center" },
  header: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingTop: 54,
    paddingBottom: 10,
    paddingHorizontal: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  backButton: { paddingHorizontal: 6 },
  backGlyph: { fontSize: 30, lineHeight: 32, fontWeight: "300" },
  headerTitleCell: { flex: 1 },
  headerTitle: { fontSize: 16, fontWeight: "600" },
  headerMeta: { fontSize: 11, marginTop: 1 },
  listContent: { paddingHorizontal: 14, paddingVertical: 12, gap: 10 },
  userRow: { alignItems: "flex-end", gap: 3 },
  // The tucked bottom-right corner is the "this is you" signature.
  userBubble: {
    maxWidth: "72%",
    paddingHorizontal: 12,
    paddingVertical: 8,
    borderTopLeftRadius: 12,
    borderTopRightRadius: 12,
    borderBottomRightRadius: 3,
    borderBottomLeftRadius: 12,
  },
  pendingBubble: { opacity: 0.65 },
  pendingHint: { fontSize: 11 },
  agentRow: { flexDirection: "row", gap: 8, alignItems: "flex-start" },
  agentBody: { flex: 1, alignItems: "flex-start", gap: 3 },
  senderName: { fontSize: 12, paddingHorizontal: 4 },
  agentBubble: {
    maxWidth: "88%",
    paddingHorizontal: 12,
    paddingVertical: 8,
    borderRadius: 12,
  },
  bubbleText: { fontSize: 15, lineHeight: 23 },
  receipt: { fontSize: 10.5 },
  declineLine: { fontSize: 13, fontStyle: "italic", textAlign: "center", paddingHorizontal: 20 },
  runningRow: { alignItems: "flex-start", paddingLeft: 36 },
  runningText: { fontSize: 12 },
  divider: { flexDirection: "row", alignItems: "center", gap: 8, paddingVertical: 2 },
  dividerLine: { flex: 1, height: StyleSheet.hairlineWidth },
  dividerLabel: { fontSize: 12 },
  taskCard: {
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 10,
    padding: 10,
    gap: 4,
    marginHorizontal: 24,
  },
  taskTop: { flexDirection: "row", justifyContent: "space-between", gap: 8 },
  taskName: { flex: 1, fontSize: 13.5, fontWeight: "600" },
  taskStatus: { fontSize: 12 },
  taskDescription: { fontSize: 12.5, lineHeight: 18 },
  taskReplies: { fontSize: 11 },
  sendError: { fontSize: 12, textAlign: "center", paddingVertical: 4 },
  mentionSheet: {
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingVertical: 4,
  },
  mentionRow: {
    flexDirection: "row",
    justifyContent: "space-between",
    paddingHorizontal: 16,
    paddingVertical: 10,
  },
  mentionName: { fontSize: 14, fontWeight: "500" },
  mentionHint: { fontSize: 13 },
  composer: {
    flexDirection: "row",
    alignItems: "flex-end",
    gap: 8,
    paddingHorizontal: 12,
    paddingTop: 8,
    paddingBottom: 28,
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  input: {
    flex: 1,
    minHeight: 38,
    maxHeight: 120,
    borderRadius: 18,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 14,
    paddingTop: 9,
    paddingBottom: 9,
    fontSize: 15,
  },
  sendButton: {
    width: 34,
    height: 34,
    borderRadius: 17,
    alignItems: "center",
    justifyContent: "center",
  },
  sendGlyph: { color: "#ffffff", fontSize: 18, fontWeight: "700" },
  stopGlyph: { width: 12, height: 12, borderRadius: 2, backgroundColor: "#ffffff" },
});
