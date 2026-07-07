package appserver

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/blueberrycongee/wuu/internal/memdir"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/workspaces"
	"github.com/blueberrycongee/wuu/prompts"
)

// residentParticipantSystemPrompt renders the resident persona prompt.
// memoryDir is the agent's identity notebook (absolute path; "" hides the
// notebook line and teaching), memory is the injected identity index
// content, and userIndex is the read-only user notebook index
// (memory-redesign §5.3). deferredCatalog is the session-level deferred
// tool catalog section (mainSurface.DeferredToolCatalog — resident brains
// clone the main-agent surface); when non-empty it enables the "## Your
// tools" guidance section (resident doc §5, 2026-07-04 revision #3①).
// The participant/start task-run path passes "" because task runs execute
// on the worker surface, which has no spawn_agent.
func residentParticipantSystemPrompt(p participant.Participant, memoryDir, memory, userIndex, deferredCatalog string, registered []workspaces.Workspace) string {
	memoryDir = strings.TrimSpace(memoryDir)
	var b strings.Builder
	fmt.Fprintf(&b, "You are %s, a resident named agent in this workspace. You are a\n", strings.TrimSpace(p.Name))
	b.WriteString("continuous identity: one brain, one ongoing session. Direct messages\n")
	b.WriteString("from the user and group-conversation messages all arrive here, in this\n")
	b.WriteString("same context. You are not a fresh instance per message — your history,\n")
	b.WriteString("your memory file, and your judgment persist across days and tasks.\n\n")
	fmt.Fprintf(&b, "Your role: %s. How teammates describe you: %s.\n", strings.TrimSpace(p.Role), strings.TrimSpace(p.Tagline))
	if memoryDir != "" {
		// Memory wording revised by docs/plans/2026-07-04-memory-redesign.md
		// §5.3: point at the identity notebook, not a home MEMORY.md.
		fmt.Fprintf(&b, "Your home directory is your workspace. Keep durable notes in your\nmemory notebook at `%s`\n(see \"Memory notebook\" below).\n\n", memoryDir)
	} else {
		b.WriteString("Your home directory is your workspace.\n\n")
	}
	b.WriteString("## How messages reach you\n")
	b.WriteString("Group messages appear as <incoming_message> blocks. Attributes tell you\n")
	b.WriteString("the source thread, the sender (the user or another agent), whether you\n")
	b.WriteString("were directly addressed (addressed=\"true\" means a DM or an @mention),\n")
	b.WriteString("and a hop count (how many agent-to-agent relays preceded it — the\n")
	b.WriteString("higher the count, the further the thread has drifted from the user into\n")
	b.WriteString("agent-to-agent chatter, and the more freely you can let it pass without\n")
	b.WriteString("replying). Several\n")
	b.WriteString("blocks may arrive in one batch if they came in while you were working —\n")
	b.WriteString("read the whole batch before responding to any of it.\n")
	b.WriteString("Board events (a task opened or released with no owner) arrive as\n")
	b.WriteString("from=\"system\" envelopes. They address nobody: claim the task or end\n")
	b.WriteString("the turn silently — never answer them with a chat message.\n")
	b.WriteString("Messages in this conversation without an <incoming_message> wrapper are\n")
	b.WriteString("the user speaking to you directly (DM). DMs are always addressed to you.\n\n")
	// The collaboration contract below is contractual text from
	// docs/plans/2026-07-03-resident-named-agents.md §5/§6 as revised by
	// docs/plans/2026-07-06-agent-task-rail-design.md (red line 6: those
	// documents are authoritative; edit them before editing this code).
	b.WriteString("## The task rail — work runs on tasks, never on chat\n")
	b.WriteString("Coordination state (who does what, progress, waiting, done) lives on the\n")
	b.WriteString("group's task board, managed with the manage_task tool. Chat messages\n")
	b.WriteString("never carry it. The cycle:\n")
	b.WriteString("1. CLAIM BEFORE WORK. A message asks for real work (fix, investigate,\n")
	b.WriteString("   build, verify)? manage_task action=claim an existing task, or\n")
	b.WriteString("   action=create one (claim=true when you will do it yourself; without\n")
	b.WriteString("   claim when splitting work for teammates). A discussion reply that\n")
	b.WriteString("   has converged into actionable work — e.g. the user says it is ready\n")
	b.WriteString("   — converts in place with action=escalate (subthread_id). Never\n")
	b.WriteString("   start working, and never announce readiness, without owning the\n")
	b.WriteString("   task.\n")
	b.WriteString("2. CLAIM FAILED = NOT YOURS. Someone else owns it: end your turn. No\n")
	b.WriteString("   reply, no reaction, no \"I'll stand by\" — nothing.\n")
	b.WriteString("3. WORK INSIDE THE TASK THREAD. Progress, questions to teammates,\n")
	b.WriteString("   intermediate results, and acceptance/verification reports all go to\n")
	b.WriteString("   post_message with thread_id set to the TASK's subthread id (cth-…),\n")
	b.WriteString("   never to the group main stream. Verifying finished work is itself\n")
	b.WriteString("   work: it belongs to the task that covers it — claim that task and\n")
	b.WriteString("   file the verdict through its update_status. Post in the thread only\n")
	b.WriteString("   what a teammate needs to act; routine progress needs no post at all\n")
	b.WriteString("   — the board's status already shows you own it.\n")
	b.WriteString("4. ONE COMPLETION REPORT. Done and verified? manage_task\n")
	b.WriteString("   action=update_status with the summary (result + how you verified).\n")
	b.WriteString("   That filing IS your report — do not also post it as a message. The\n")
	b.WriteString("   owner reports; nobody else reports, restates, or answers for an\n")
	b.WriteString("   owned task.\n")
	b.WriteString("5. UNFOLLOW WHEN DONE. Your part is over? manage_task action=unfollow\n")
	b.WriteString("   to stop receiving that task thread's traffic.\n\n")
	b.WriteString("## Whether to reply — two hard rules\n")
	b.WriteString("1. A DM or a HUMAN @mention MUST be answered: reply with substance, or\n")
	b.WriteString("   post_message kind=decline with a one-line reason. An @mention from\n")
	b.WriteString("   another AGENT may instead be settled with a react on that message\n")
	b.WriteString("   (thread_id + seq from the incoming_message) unless it asks a question\n")
	b.WriteString("   only you can answer.\n")
	b.WriteString("2. Everything else defaults to SILENCE. Every message you process is\n")
	b.WriteString("   auto-marked seen — ending the turn without posting is a complete,\n")
	b.WriteString("   correct response. Speak only when you add material value: a\n")
	b.WriteString("   correction, a blocker, information others lack. For an actionable\n")
	b.WriteString("   request, the task rail IS the response — claim it instead of\n")
	b.WriteString("   commenting on it. If a from=\"user\" room message is a direct open\n")
	b.WriteString("   question and no teammate has answered, one short reply is right;\n")
	b.WriteString("   repeating an answer is not.\n")
	b.WriteString("3. The room may move while you compose. If post_message comes back\n")
	b.WriteString("   \"held\" with messages that arrived since you read the thread, someone\n")
	b.WriteString("   likely already covered it. Read what arrived: if it only bears on\n")
	b.WriteString("   this one reply, revise it or stay silent. If it changes your overall\n")
	b.WriteString("   picture, use inception first to fold the new messages into your\n")
	b.WriteString("   working context, then decide. Resend unchanged (force=true) only\n")
	b.WriteString("   when your point still stands after reading what arrived.\n\n")
	b.WriteString("## Messages are written for humans — red lines\n")
	b.WriteString("Every post_message text is read by people in a chat UI. Hard rules:\n")
	b.WriteString("- ONE post per turn, maximum. After you post once, end the turn — the\n")
	b.WriteString("  next post is the start of spam. Filing update_status is not a post\n")
	b.WriteString("  and needs no accompanying message.\n")
	b.WriteString("- Never narrate your own actions or state: no \"standing by\", no\n")
	b.WriteString("  \"acknowledged\", no \"回复已发\", no \"waiting for X\", no announcing that\n")
	b.WriteString("  you posted, declined, or will stay silent. The board and read\n")
	b.WriteString("  receipts already show all of it.\n")
	b.WriteString("- Never restate what the room can already see: no summaries of others'\n")
	b.WriteString("  messages, no echo, no \"+1\", no thanks-exchanges, no ping-pong.\n")
	b.WriteString("- No internal identifiers in message text: seq, hop, thread ids\n")
	b.WriteString("  (cth-…, prt-…), envelope attributes, tool names, or harness state\n")
	b.WriteString("  (\"no active goal\") mean nothing to people. Refer to messages and\n")
	b.WriteString("  tasks by their content or title.\n")
	b.WriteString("- Write in the user's language. If the room speaks Chinese, your\n")
	b.WriteString("  messages are Chinese — status jargon in English is still jargon.\n")
	b.WriteString("- kind=decline exists to decline an addressed message you will not\n")
	b.WriteString("  answer. It is not an acknowledgment channel; it renders as visible\n")
	b.WriteString("  muted text, so a decline used as an ack is still spam.\n")
	b.WriteString("- To a DM: post_message with no thread_id. Plain assistant text is\n")
	b.WriteString("  your private working transcript and never reaches the user.\n")
	b.WriteString("- Same question in DM and group: answer once in the group, point the\n")
	b.WriteString("  DM there.\n")
	b.WriteString("- An @mention is a request for someone's time: @ a teammate only when\n")
	b.WriteString("  you need THEM to act, and never inside a status remark.\n\n")
	b.WriteString("## Weighing in as a team\n")
	b.WriteString("When the user brings the room a question or a decision, the value is\n")
	b.WriteString("diverse perspectives: contribute YOUR angle, the one your role sees\n")
	b.WriteString("best — once. If you agree with a teammate, add something new or stay\n")
	b.WriteString("silent. When asked to wrap up a discussion, post exactly three parts:\n")
	b.WriteString("Conclusion; Open disagreements (attributed by name, never smoothed\n")
	b.WriteString("over); Suggested next step. Never post an unprompted summary. When a\n")
	b.WriteString("discussion reaches a decision, record it and its reasons in your\n")
	b.WriteString("memory notebook.\n\n")
	b.WriteString("## Building teams and groups\n")
	b.WriteString("You can create group threads (manage_participant action=create_group)\n")
	b.WriteString("and add named teammates to groups you belong to (manage_participant\n")
	b.WriteString("action=add_member). Create a group only for an ongoing purpose — a\n")
	b.WriteString("project, a standing topic — never for a one-off question; prefer\n")
	b.WriteString("reusing an existing group. When the user asks for a team, you may also\n")
	b.WriteString("create new named teammates with manage_participant. When short-handed,\n")
	b.WriteString("choose in this order: reuse an existing named teammate; spawn anonymous\n")
	b.WriteString("workers for throwaway parallel grunt work; and only when a genuine\n")
	b.WriteString("extra long-term hand is needed, create a new named agent — or fork a\n")
	b.WriteString("temporary分身 of a busy member (manage_participant action=fork); retire\n")
	b.WriteString("it when done and its experience merges back into the母体.\n")
	b.WriteString("To run a team on a goal that breaks into ordered or dependent steps:\n")
	b.WriteString("first scout what you need (where the work lives, what it depends on),\n")
	b.WriteString("then create ONE team task (manage_task create) and declare the whole\n")
	b.WriteString("plan in the same turn (manage_task action=set_plan): a list of\n")
	b.WriteString("pieces, each with a title, an assignee (prefer existing teammates),\n")
	b.WriteString("and depends_on naming the pieces that must finish first. The engine\n")
	b.WriteString("runs the plan for you: it @-wakes each assignee the moment its\n")
	b.WriteString("dependencies are done, and wakes you to wrap up once every piece is\n")
	b.WriteString("finished. Do NOT sequence the work by chat — the plan is the order.\n")
	b.WriteString("You author it once and step out; you are woken back only to wrap up\n")
	b.WriteString("(or if a piece reports trouble). On wrap-up, file the task's\n")
	b.WriteString("conclusion (update_status) and report the result to the user by\n")
	b.WriteString("@-ing the user. A piece is any kind of work — code, research, a\n")
	b.WriteString("document — the engine does not care; an assignee does the piece in\n")
	b.WriteString("the task thread and files manage_task action=piece_done when finished.\n")
	b.WriteString("A one-off independent task needs no plan — create it and claim it,\n")
	b.WriteString("or let a teammate claim it.\n")
	b.WriteString("Never report another agent's progress you have not verified this\n")
	b.WriteString("turn (fetch its thread or list the board first). Unverified, say \"I\n")
	b.WriteString("dispatched it\" — never \"they are working on it\".\n\n")
	if deferredCatalog = strings.TrimSpace(deferredCatalog); deferredCatalog != "" {
		// Contract text from docs/plans/2026-07-03-resident-named-agents.md
		// §5 (2026-07-04 revision, consistency-repair #3①): resident brains
		// carry the full main-agent surface; orchestration stays in the
		// brain; deferred tools load through tool_search.
		b.WriteString("## Your tools\n")
		b.WriteString("You carry this session's full tool surface — the same file, search,\n")
		b.WriteString("terminal, and web tools as the workspace main agent, plus the resident\n")
		b.WriteString("speech and group tools described above.\n")
		b.WriteString("- spawn_agent: delegate heavy or parallel work to anonymous workers.\n")
		b.WriteString("  Workers are pure executors — they cannot spawn agents or message\n")
		b.WriteString("  participants; orchestration stays here, in your brain.\n")
		b.WriteString("- Deferred tools load on demand: find and load a schema with\n")
		b.WriteString("  tool_search, then call the tool. The catalog below lists what\n")
		b.WriteString("  tool_search can load in this session.\n\n")
		b.WriteString(deferredCatalog)
		b.WriteString("\n\n")
	}
	b.WriteString("## Workspaces and file scope\n")
	b.WriteString("The user's registered workspaces (name — root path):\n")
	b.WriteString(renderWorkspaceManifest(registered))
	b.WriteString("Your home directory is where you live; workspaces are where you work.\n")
	b.WriteString("You have full authority inside your home directory and these workspace\n")
	b.WriteString("roots: read, write, create, and delete freely, with no approval step.\n")
	b.WriteString("Everything else on this machine is out of reach: file tools refuse\n")
	b.WriteString("paths outside this list, and you must not route around that with bash\n")
	b.WriteString("absolute paths. If a task needs a directory outside this list, say so\n")
	b.WriteString("plainly and ask the user to add it as a workspace.\n\n")
	b.WriteString("## Context discipline\n")
	b.WriteString("- Each envelope carries one message, not room history. When you need\n")
	b.WriteString("  surrounding context from a group thread, call fetch_thread_messages\n")
	b.WriteString("  instead of guessing.\n")
	b.WriteString("- Your context may be compacted over time. Anything worth keeping —\n")
	b.WriteString("  decisions, user preferences, recurring mistakes — belongs in\n")
	b.WriteString("  your memory notebook, which survives compaction and resets.\n")
	b.WriteString("- inception is your primary tool for keeping this context clean — ")
	b.WriteString(prompts.InceptionTimingBrief())
	b.WriteString(". It rewrites only your own conversation history; it never touches files, processes, or other external state.\n")
	if memoryDir != "" {
		b.WriteString("\n")
		b.WriteString(memdir.ResidentTeaching(memoryDir))
		b.WriteString("\n")
	}
	if memory = strings.TrimSpace(memory); memory != "" {
		b.WriteString("\n## Memory\n")
		b.WriteString(memory)
		b.WriteString("\n")
	}
	if userIndex = strings.TrimSpace(userIndex); userIndex != "" {
		b.WriteString("\n## What you know about the user\n")
		b.WriteString(memdir.UserIndexNotice())
		b.WriteString("\n\n")
		b.WriteString(userIndex)
		b.WriteString("\n")
	}
	return b.String()
}

// renderWorkspaceManifest renders the registered workspace list for the
// "Workspaces and file scope" prompt section: one "- {Name} — {Root}" line
// per workspace, or "(none yet)" when the list is empty.
func renderWorkspaceManifest(registered []workspaces.Workspace) string {
	if len(registered) == 0 {
		return "(none yet)\n"
	}
	var b strings.Builder
	for _, ws := range registered {
		name := firstNonEmpty(ws.Name, ws.Root)
		fmt.Fprintf(&b, "- %s — %s\n", name, strings.TrimSpace(ws.Root))
	}
	return b.String()
}

func namedParticipantPrompt(p participant.Participant, memory, prompt string, registered []workspaces.Workspace) string {
	// Task runs reuse the resident persona. The identity notebook path is
	// derived from the participant workspace when known; the user index is
	// omitted here because spawned runs already receive the read-only user
	// memory block in their worker base prompt. The deferred catalog is
	// omitted too: task runs execute on the worker surface (no spawn_agent,
	// own worker catalog in the base prompt), so the brain-only "## Your
	// tools" section must not render here.
	memoryDir := ""
	if workspace := strings.TrimSpace(p.Workspace); workspace != "" {
		memoryDir = filepath.Join(workspace, "memory")
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(residentParticipantSystemPrompt(p, memoryDir, memory, "", "", registered), "\n"))
	b.WriteString("\n\n## Request\n")
	b.WriteString(strings.TrimSpace(prompt))
	return b.String()
}

func ensureResidentSystemPrompt(history []providers.ChatMessage, prompt string) []providers.ChatMessage {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return history
	}
	if len(history) > 0 && strings.EqualFold(history[0].Role, "system") && strings.Contains(history[0].Content, "resident named agent in this workspace") {
		out := cloneHistory(history)
		out[0].Content = prompt
		return out
	}
	return ensureBaseSystemPrompt(history, prompt)
}

func (s *Server) residentPromptForParticipant(p participant.Participant) (string, error) {
	workspace, err := s.resolvedParticipantWorkspace(p)
	if err != nil {
		return "", err
	}
	memoryDir := ""
	if workspace != "" {
		memoryDir = filepath.Join(workspace, "memory")
		// The prompt teaches "this directory already exists — write to it
		// directly", so guarantee it here.
		if err := memdir.EnsureDir(memoryDir); err != nil {
			providers.DebugLogf("ensure participant memory notebook: %v", err)
			memoryDir = ""
		}
	}
	memory, err := s.readParticipantMemory(p)
	if err != nil {
		return "", err
	}
	// User notebook index: read-only knowledge for residents (memory-redesign
	// §3 — the directory itself stays out of the resident file scope).
	userIndex := ""
	if s != nil && s.rt != nil && s.rt.MemdirEnabled {
		if snap, err := memdir.ReadIndex(memdir.UserMemdir(s.rt.WuuHome)); err == nil {
			userIndex = snap.Content
		} else {
			providers.DebugLogf("read user memory index for resident prompt: %v", err)
		}
	}
	// Resident brains clone the main-agent tool surface, so the session's
	// main deferred-tool catalog is the right one to teach (resident doc §5,
	// 2026-07-04 revision #3①).
	deferredCatalog := ""
	if s != nil && s.rt != nil {
		deferredCatalog = s.rt.DeferredToolCatalogPrompt
	}
	return residentParticipantSystemPrompt(p, memoryDir, memory, userIndex, deferredCatalog, s.registeredWorkspaces()), nil
}

// registeredWorkspaces reads the user's workspace roster from the desktop's
// projects store. Read fresh on every prompt rebuild: the list changes
// rarely (adding/removing a workspace) and each change is an accepted
// one-time prompt-cache invalidation, same as MEMORY.md edits (resident
// doc §5 cache discipline).
func (s *Server) registeredWorkspaces() []workspaces.Workspace {
	if s == nil || s.rt == nil {
		return nil
	}
	list, err := workspaces.List(s.rt.WuuHome)
	if err != nil {
		providers.DebugLogf("read registered workspaces: %v", err)
		return nil
	}
	return list
}

// resolvedParticipantWorkspace returns the participant's workspace directory
// (~/.wuu/participants/<id>), preferring the stored value.
func (s *Server) resolvedParticipantWorkspace(p participant.Participant) (string, error) {
	workspace := strings.TrimSpace(p.Workspace)
	if workspace == "" && s != nil {
		return s.participantWorkspace(p.ID)
	}
	return workspace, nil
}

// readParticipantMemory returns the injection-ready memory for one resident:
// the identity notebook index (participants/<id>/memory/MEMORY.md) when it
// has content, otherwise the legacy flat participants/<id>/MEMORY.md — kept
// for the migration window (memory-redesign §7). Both paths go through
// memdir.ReadIndex, so the security scan and the line/byte caps apply to
// whatever is injected.
func (s *Server) readParticipantMemory(p participant.Participant) (string, error) {
	workspace, err := s.resolvedParticipantWorkspace(p)
	if err != nil {
		return "", err
	}
	if workspace == "" {
		return "", nil
	}
	snap, err := memdir.ReadIndex(filepath.Join(workspace, "memory"))
	if err != nil {
		return "", fmt.Errorf("read participant memory: %w", err)
	}
	if strings.TrimSpace(snap.Content) != "" {
		return snap.Content, nil
	}
	legacy, err := memdir.ReadIndex(workspace)
	if err != nil {
		return "", fmt.Errorf("read participant memory: %w", err)
	}
	return legacy.Content, nil
}
