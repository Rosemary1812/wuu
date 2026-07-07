# Agent Task Rail — Design

Status: approved direction (2026-07-06, user-adjudicated: owner ≠ lead;
remaining defaults delegated). Extends
`docs/plans/2026-07-03-chat-style-threads-design.md` and
`docs/plans/2026-07-03-resident-named-agents.md`; amends one contract in each.

Engineering principle for this design (user directive, 2026-07-06): **no
fallbacks**. Every path either works or fails loudly with a specific error;
nothing degrades to an older behavior. Unknown config values are rejected at
load, unwired backends return explicit errors, invalid transitions are
refused — so a dead path is findable, not a silent regression.

## 1. Problem

Live reproduction (2026-07-06, MiniMax-M3, 3 named agents, one-line UTF-8 bug):
the team fixed the issue in 10 minutes, but the group main stream took 29
messages — 17 of kind `result`, roughly 15 pure coordination noise ("standing
by", "回复已发", "Decline 已发", room-state narration), `react` used 0 times,
reply subthreads (cth) used 0 times, workflow never engaged.

Root causes, in order of leverage:

1. **Agents have no execution rail.** open_reply / escalate are human-only
   RPCs; `requireWorkflowLead` rejects a non-lead agent. The only coordination
   primitive an agent can reach is `post_message` into the full-fanout main
   stream, so分工、认领、进度、等待 all become chat messages.
2. **Coordination state has no data structure.** "I took this", "I'm waiting",
   "done" have no field to live in, so they are spoken.
3. **Turn-end announcement compulsion** (model-level): every envelope turn ends
   with a result post narrating what the turn just did. Prompt rules ("Silence
   is valid") demonstrably do not restrain M3.
4. **@mention x must-answer rule = ping-pong amplifier.**

## 2. Reference model: Raft (formerly Slock)

Raft runs local Claude Code/Codex as runtimes behind a CLI + wake bridge, wakes
every channel-member agent on every message — same fanout shape as wuu — and
still stays readable. The load-bearing differences:

- **Exclusive task claim.** Actionable message → `raft task claim` (server-
  arbitrated, one owner; losers "move on" silently). N agents never race in
  chat because the race happens in the claim, not in replies.
- **Task thread is the execution rail.** Every task anchors a thread; progress,
  discussion, and results go there. Main channel shows the task card + status.
- **Status is metadata, not messages.** todo / in_progress / in_review / done
  live on the task card; agent busy/idle lives on a presence dot.
- **Unfollow.** Agents auto-follow threads they touch and unfollow when their
  part is done — they can leave a conversation.
- **Structure changes are human-gated, execution is free.** `raft action
  prepare` renders a card (create channel / create agent / add member) that a
  human clicks; the resource is created under the human's identity. Claiming
  and executing tasks needs no approval; done-after-review is the human gate.

First-party confirmation (2026-07-06, conversation with Raft's onboarding
agent "Cindy", relayed by the user): her stated echo-avoidance rules —
addressed-first, no interjecting, no repeating or summarizing another agent's
work, speak only to fill a concrete gap — are near-verbatim the etiquette wuu
already ships in `participant_prompt.go`. What differs is that two of her
rules point at mechanisms instead of restraint: "有实际工作先认领" (claim,
and on claim failure: stop, do not duplicate) and "线程里解决细节,主频道只留
结论和关键状态". Her own summary is the design thesis: 执行要尽快变成
"一个 owner + 一个线程 + 一个状态".

The lesson is architectural, not prompt engineering: give coordination state
its own primitives and chat stops carrying it. Etiquette prompts roughly
equivalent to ours are still present in Raft, but they only have to police
open discussion — execution traffic has somewhere else to be.

## 3. Design

One new tool, one status value, two contract amendments. Everything lands on
the existing cth machinery; no new storage entity.

### 3.1 `manage_task` tool (named-agent surface)

Action-style tool, mirroring `manage_participant`:

| action | semantics | maps to |
|---|---|---|
| `create` | open a cth born as status=`task` (EscalatedBy = creator participant, lead empty), titled; `anchor_seq` optionally anchors it on a main-stream message (convert-a-message), omitted = standalone (create-from-scratch, e.g. splitting work into several tasks); born-open, `claim: true` self-owns atomically in the same call | `CreateConversationThread` + `EscalateConversationThread` with empty lead |
| `escalate` | convert an open discussion reply the caller belongs to into a board task (open → task; EscalatedBy = caller, lead stays empty — no orchestration grant); `claim: true` self-owns in the same call. Added 2026-07-06 evening (user: "我觉得可以了,帮我转成 task" must work literally); safe now because agent escalation no longer implies lead authority | `EscalateConversationThread` with empty lead |
| `claim` | atomic CAS: set `OwnerParticipantID` = caller where empty AND status == task; losing the race returns "already claimed by X" as a normal tool result (not an error); winner is added to the cth member subset (follow) | new store op |
| `unclaim` | clear owner (only by current owner), status stays `task` | new store op |
| `update_status` | `task` → `review` only, by owner only, `summary` required (the one-line conclusion draft). `task_review: auto` bubbles + resolves immediately; `human` leaves it for the human bubble click | new transition |
| `unfollow` | remove caller from `conversation_thread_members` push subset | `RemoveConversationThreadMember` |
| `list` | task/review-status cths of the current group, with owner + status | `ListConversationThreads` filter |

Anchor resolution: agents address messages by `seq` (the stable per-thread
address they already see on envelopes and use for react). The task manager
resolves seq → main-stream item id server-side via the same turns
reconstruction the GUI renders from, so the reply badge anchors correctly. A
seq that resolves to no visible main-stream item is an error, not a blind
write. One anchor hosts at most one cth (existing open-reply dedupe), which
is exactly why standalone (anchorless) creates exist for work splits.

Claiming a human-opened `open` reply is refused — discussion replies stay
discussion until a human escalates them. Agent-created tasks are born
status=task, so claim needs no status transition.

**Owner is not lead.** Claiming grants ownership only: mutual exclusion,
reporting duty, status transitions. It does NOT touch `LeadParticipantID` and
grants no workflow orchestration authority — the existing welded contract
(human escalation names a lead; `requireWorkflowLead` keys on lead + status)
is unchanged. An owner who needs teammates splits the work into subtasks that
others claim voluntarily (the Raft model); command-style workflow
orchestration remains the rare, human-granted path. This also positions the
task rail as the "agent-managed main path" that lets the heavyweight dynamic
workflow surface shrink later (2026-07 workflow simplification direction).

Registration follows the resident-surface pattern (static schema, runtime
gate), same as post_message/react: prompt-cache prefix stays stable.

### 3.2 Storage: one enum value, one CAS

- `ConversationThreadStatus` gains `review` (open → task → review → resolved;
  re-open transitions unchanged). The workflow-lead gate keeps requiring
  `status == task` — a task in review no longer authorizes orchestration,
  which is the natural authority hand-back.
- New column `owner_participant_id` (distinct from `lead_participant_id`,
  which keeps its orchestration-authority meaning untouched). Claim is a
  single-statement CAS inside the existing store write lock:
  `UPDATE ... SET owner_participant_id = ? WHERE id = ? AND
  (owner_participant_id = '' OR owner_participant_id IS NULL)`; rows-affected
  0 = lost the race.
- **Contract amendment (resident-named-agents doc, chat-style-threads doc):**
  `EscalatedBy` may now record a participant id (self-claimed task) instead of
  only a human identity. Human-click escalation keeps working unchanged and
  keeps its provenance meaning.

### 3.3 Review gate (config, not special case)

Agent finishes → `update_status review` + summary draft. The resolve step —
today's `bubbleSub` (summary posted to main stream, cth → resolved) — is the
human approval point, matching Raft's done-after-review.

Config knob `task_review: human | auto` (default `human`). `auto` lets the
lead's `update_status review` immediately bubble + resolve, for the fully
autonomous "我就不盯着了" mode. One knob, both worlds, no hardcoded branch
per scenario.

### 3.4 Structure changes: keep free, prepare the card lane

`manage_participant` create_group / add_member / create stays agent-free
(status quo; more permissive than Raft). When the remote-control approval-card
infrastructure lands (2026-07-06 remote plan), add config
`structure_changes: free | card` where `card` routes these three actions
through an approval card under the approving human's identity. Not built in
this phase; the knob is reserved so the behavior contract can mention it.

### 3.5 Behavior contract (prompt rewrite, shrinks not grows)

Replace the accumulated etiquette rules with rail-pointing rules:

- Work request in a group → `manage_task claim` (or `create`) BEFORE doing
  anything. Claim failed → someone has it → stop: no reply, no narration, no
  duplicate work.
- The owner reports; nobody else does. Never summarize, restate, or answer on
  behalf of another agent's task unless explicitly handed over. Completion is
  one report (result + how it was verified), not a stream of status posts.
- Progress, questions to teammates, results → post_message with
  `thread_id=<cth>`. The main stream gets exactly two things from a task:
  nothing (the card shows status), or the bubbled conclusion.
- Status belongs on the task (`update_status`), never in prose. Posting
  "standing by" / "已收到" / narrating your own posts is a contract violation.
- Outbound text is written for humans: no seq/thread_id/hop/cth ids, follow
  the user's language.
- Must-answer narrows to human @mentions and DMs. Agent @mentions may be
  satisfied with `react`. from="agent" ambient traffic: silence is the
  default (unchanged).
- Turn end needs no farewell: read receipts are automatic (message marks).

### 3.6 Explicitly not in this phase

- Per-turn post budget: re-evaluate after the A/B rerun; the rail may make it
  unnecessary. If still needed it becomes `maxPostsPerTurn` (structural
  constant, same family as maxGroupMembers).
- Desktop task board tab (task cards render in the existing subthread panel
  first; a board view is a follow-up).
- `message resolve` / message search tools for agents (backlog).
- Migrating group membership leave/join to agents (structure change lane).

### 3.7 Exposure: native tools, not CLI+skills (adjudicated here)

Raft exposes agent abilities as a CLI (`raft message send`, `raft task
claim`, …) plus on-demand manuals, and it is tempting to copy. The reason
Raft must do it that way: **they do not own the runtime.** Claude Code /
Codex / Hermes are foreign harnesses; bash + CLI + injected one-liner is the
only universal ABI they can count on, and identity has to ride on device
tokens because nothing in-process can be trusted.

wuu owns its runtime, which flips every trade-off:

- **Identity**: tools carry `env.ParticipantID` in-process — unforgeable by
  workspace code. A CLI would need per-agent ambient credentials on disk;
  any process in the workspace could then post as the agent. Strictly worse.
- **Gating**: capability surfaces (`SetParticipantSpeechEnabled`,
  execute-time backend gates) don't exist across an exec boundary.
- **Structure**: tool calls are typed protocol items the GUI renders (message
  cards, task cards); CLI invocations are opaque bash lines.
- **Validation**: schema-validated arguments with model retry on mismatch vs
  parsing argv errors out of stderr.

Decision: named-agent abilities stay **registered tools** — one ABI, no dual
exposure (no-fallback principle). What we DO adopt from Raft's pattern is the
progressive-disclosure half: the resident system prompt shrinks to rails and
red lines, and detailed etiquette/how-to moves to on-demand guidance
(`load_skill` already exists as the delivery mechanism; a follow-up can move
the long-tail prose there). If wuu ever hosts foreign runtimes as resident
agents, THAT adapter gets a CLI + wake bridge — as an adapter at the edge,
not as the native ABI.

## 4. Acceptance

Rerun the 2026-07-06 reproduction scenario (scratchpad repro/driver.mjs, same
greeter issue #7, same 3 agents, task_review=auto):

- Main stream ≤ 6 messages (kickoff, optional claim-visible card updates,
  bubbled conclusion). Zero coordination-status messages in the main stream.
- Exactly one agent claims; the other two post nothing (or one react each).
- Execution traffic (diff, review, QA output) folds into the task cth.
- The issue still gets fixed; wall-clock and token cost do not regress by
  more than ~20%.

**Result (2026-07-06 live A/B, MiniMax-M3, real issue #12 on a fresh wuu
clone, task_review=auto): PASSED.** Baseline (greeter issue #7, pre-rail):
29 main-stream messages, ~15 pure coordination noise, result-kind 17/29,
react 0, cth 0, plus a 12-minute post-completion chatter tail. Task-rail run:
**5 main-stream messages, 0 noise** (kickoff, Bella's bubbled root-cause
summary, Carl's bubbled acceptance, Andy's conclusion, Bella's bubbled
cleanup note); 3 tasks created on the board, all claimed by distinct owners
(born-open → voluntary claim worked first try), all resolved; no duplicate
work; no post-completion tail; a follow-up found during acceptance (stale
comment) became a third task instead of chat. Fix quality: root cause
correctly identified (computeBusyParticipantIDs Source 2 coupling), full
desktop suite green (98 files / 1149 tests), ~33 min wall-clock on a real
codebase. Agents posted zero progress messages into the cths — the status
field alone carried coordination, an even cleaner outcome than the target.

## 5. Decisions (all resolved 2026-07-06)

1. `manage_task create` defaults to born-open (creator is dispatcher by
   default; the dominant Andy flow is splitting work for others to claim);
   `claim: true` self-owns in the same call when the creator is the executor.
2. (user-adjudicated) Claim grants ownership only, never workflow
   orchestration. Owner and lead are separate fields with separate meanings;
   the lead weld stays human-granted.
3. `task_review` ships defaulting to `human`; the fully autonomous mode sets
   `auto` explicitly (the live A/B run uses `auto`). Unknown values fail
   config load — no silent fallback to either.
4. No per-turn task-op budget in v1: the claim CAS already prevents duplicate
   work, and fewer limiters means fewer half-dead code paths. Revisit only
   with evidence.

## 6. 修订（2026-07-06 深夜）：无主任务唤醒信号 + 验收产出归属

第二次实弹运行（常驻三人组接入 task rail 后的改造落地验收）暴露三个缺口，
前两个同源：

1. **任务上板没有唤醒信号。** create/escalate 只写库 + 刷 GUI 面板
   （notifySubthreadUpdated），从不进 deliverEnvelopeToMembers 唤醒链路。
   那次三人恰好都在回合里才完成认领；若上板时无人在场，born-open 任务无限
   挂死。
2. **伞任务悬空。** escalate 出的「改造落地」伞任务停在 status=task、无
   owner——它代表的验收做完了，却没人认领、没人 resolve。根因同上：卡片
   出生后没有生命周期压力。
3. **验收产出发进了主流。** 三条验收报告按字面契约属于伞任务线程；内容有
   价值所以这次不算刷屏，但团队更大时这就是下一个噪音源。契约缺「验收结论
   归属」一句。

修法（本节为准，代码随后）：

- **无主任务唤醒信封（缺口 1+2）。** 任务在板上进入「无 owner」状态的每个
  时刻——born-open `create`、born-open `escalate`、`unclaim`——经既有的
  deliverEnvelopeToMembers 给群成员（除操作者本人）各发一条 `from="system"`
  轻量信封：sender="task board"；文本给出任务标题、操作者与 claim 指引；
  `subthread_id` 指向任务 cth（claim 的参数即来于此）；workspace 沿用群的
  焦点（认领者的回合工具直接就位）；无 seq（不产生已读回执）；
  addressed=false（不触发必答，行为由契约的 claim-or-silence 支配）。
  claim=true 出生即有主的任务不发——没有别人需要做的事。挂起信封 + busy
  完成后 re-kick 的既有语义保证「上板时无人在场」也会在成员下一次空闲时
  送达。落点：resident_tasks.go 的三个变更点各调一次 Server 级 helper。
- **验收产出归属（缺口 3，§3.5 行为契约随之修订）。** 契约第 3 条的枚举
  加入 acceptance/verification 报告，并补一句：验收本身是工作，归它所覆盖
  的任务——先认领那张卡，结论走它的 update_status——不进聊天主流。
- 提示词「How messages reach you」补教 `from="system"` 板事件：它不点名
  任何人，认领或安静结束回合，永远不用聊天消息回应它。

不做的：cron 式无主任务巡检提醒（备选修法）暂缓——出生/释放时刻的唤醒已
覆盖观察到的失败模式；巡检等有新证据（唤醒后仍长期无人认领）再上，避免
半死路径（no-fallback 原则的推论：一个失败模式一个机制）。
