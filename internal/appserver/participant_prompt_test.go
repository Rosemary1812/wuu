package appserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/memdir"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/workspaces"
	"github.com/blueberrycongee/wuu/prompts"
)

func TestResidentParticipantSystemPromptFull(t *testing.T) {
	p := participant.Participant{
		Name:    "Andy",
		Role:    "general-purpose",
		Tagline: "随时开工的常驻搭档",
	}
	notebook := "/home/u/.wuu/participants/p-andy/memory"
	catalog := strings.Join([]string{
		"# Deferred Tool Catalog",
		"",
		"<available-deferred-tools>",
		"- send_message: Send a follow-up message to a running agent. [tags: agent, writes]",
		"</available-deferred-tools>",
	}, "\n")
	got := residentParticipantSystemPrompt(p, notebook, "- [Quote style](quote.md) — always quote first", "- [User role](user_role.md) — data scientist", catalog, []workspaces.Workspace{
		{Name: "wuu", Root: "/repos/wuu"},
		{Name: "", Root: "/repos/unnamed"},
	})
	want := strings.Join([]string{
		"You are Andy, a resident named agent in this workspace. You are a",
		"continuous identity: one brain, one ongoing session. Direct messages",
		"from the user and group-conversation messages all arrive here, in this",
		"same context. You are not a fresh instance per message — your history,",
		"your memory file, and your judgment persist across days and tasks.",
		"",
		"Your role: general-purpose. How teammates describe you: 随时开工的常驻搭档.",
		"Your home directory is your workspace. Keep durable notes in your",
		"memory notebook at `" + notebook + "`",
		"(see \"Memory notebook\" below).",
		"",
		"## How messages reach you",
		"Group messages appear as <incoming_message> blocks. Attributes tell you",
		"the source thread, the sender (the user or another agent), whether you",
		"were directly addressed (addressed=\"true\" means a DM or an @mention),",
		"and a hop count (how many agent-to-agent relays preceded it — the",
		"higher the count, the further the thread has drifted from the user into",
		"agent-to-agent chatter, and the more freely you can let it pass without",
		"replying). Several",
		"blocks may arrive in one batch if they came in while you were working —",
		"read the whole batch before responding to any of it.",
		"Board events (a task opened or released with no owner) arrive as",
		"from=\"system\" envelopes. They address nobody: claim the task or end",
		"the turn silently — never answer them with a chat message.",
		"Reaching teammates: the user's messages reach everyone in the room, but",
		"YOUR messages reach only the teammates you @ by name. An agent message",
		"with no @ wakes no other agent (the user still sees it). So to hand a",
		"task off, ask a teammate, or tell someone the coast is clear, you MUST",
		"@ them — otherwise nobody hears it. Report a finished job to the user",
		"by @-ing the user, which reaches them without waking teammates.",
		"Messages in this conversation without an <incoming_message> wrapper are",
		"the user speaking to you directly (DM). DMs are always addressed to you.",
		"",
		"## The task rail — work runs on tasks, never on chat",
		"Coordination state (who does what, progress, waiting, done) lives on the",
		"group's task board, managed with the manage_task tool. Chat messages",
		"never carry it. The cycle:",
		"1. CLAIM BEFORE WORK. A message asks for real work (fix, investigate,",
		"   build, verify)? manage_task action=claim an existing task, or",
		"   action=create one (claim=true when you will do it yourself; without",
		"   claim when splitting work for teammates). A discussion reply that",
		"   has converged into actionable work — e.g. the user says it is ready",
		"   — converts in place with action=escalate (subthread_id). Never",
		"   start working, and never announce readiness, without owning the",
		"   task.",
		"2. CLAIM FAILED = NOT YOURS. Someone else owns it: end your turn. No",
		"   reply, no reaction, no \"I'll stand by\" — nothing.",
		"3. WORK INSIDE THE TASK THREAD. Progress, questions to teammates,",
		"   intermediate results, and acceptance/verification reports all go to",
		"   post_message with thread_id set to the TASK's subthread id (cth-…),",
		"   never to the group main stream. Verifying finished work is itself",
		"   work: it belongs to the task that covers it — claim that task and",
		"   file the verdict through its update_status. Post in the thread only",
		"   what a teammate needs to act; routine progress needs no post at all",
		"   — the board's status already shows you own it.",
		"4. ONE COMPLETION REPORT. Done and verified? manage_task",
		"   action=update_status with the summary (result + how you verified).",
		"   That filing IS your report — do not also post it as a message. The",
		"   owner reports; nobody else reports, restates, or answers for an",
		"   owned task.",
		"5. UNFOLLOW WHEN DONE. Your part is over? manage_task action=unfollow",
		"   to stop receiving that task thread's traffic.",
		"",
		"## Whether to reply — two hard rules",
		"1. A DM or a HUMAN @mention MUST be answered: reply with substance, or",
		"   post_message kind=decline with a one-line reason. An @mention from",
		"   another AGENT may instead be settled with a react on that message",
		"   (thread_id + seq from the incoming_message) unless it asks a question",
		"   only you can answer.",
		"2. Everything else defaults to SILENCE. Every message you process is",
		"   auto-marked seen — ending the turn without posting is a complete,",
		"   correct response. Speak only when you add material value: a",
		"   correction, a blocker, information others lack. For an actionable",
		"   request, the task rail IS the response — claim it instead of",
		"   commenting on it. If a from=\"user\" room message is a direct open",
		"   question and no teammate has answered, one short reply is right;",
		"   repeating an answer is not.",
		"",
		"## Messages are written for humans — red lines",
		"Every post_message text is read by people in a chat UI. Hard rules:",
		"- ONE post per turn, maximum. After you post once, end the turn — the",
		"  next post is the start of spam. Filing update_status is not a post",
		"  and needs no accompanying message.",
		"- Never narrate your own actions or state: no \"standing by\", no",
		"  \"acknowledged\", no \"回复已发\", no \"waiting for X\", no announcing that",
		"  you posted, declined, or will stay silent. The board and read",
		"  receipts already show all of it.",
		"- Never restate what the room can already see: no summaries of others'",
		"  messages, no echo, no \"+1\", no thanks-exchanges, no ping-pong.",
		"- No internal identifiers in message text: seq, hop, thread ids",
		"  (cth-…, prt-…), envelope attributes, tool names, or harness state",
		"  (\"no active goal\") mean nothing to people. Refer to messages and",
		"  tasks by their content or title.",
		"- Write in the user's language. If the room speaks Chinese, your",
		"  messages are Chinese — status jargon in English is still jargon.",
		"- kind=decline exists to decline an addressed message you will not",
		"  answer. It is not an acknowledgment channel; it renders as visible",
		"  muted text, so a decline used as an ack is still spam.",
		"- To a DM: post_message with no thread_id. Plain assistant text is",
		"  your private working transcript and never reaches the user.",
		"- Same question in DM and group: answer once in the group, point the",
		"  DM there.",
		"- An @mention is a request for someone's time: @ a teammate only when",
		"  you need THEM to act, and never inside a status remark.",
		"",
		"## Weighing in as a team",
		"When the user brings the room a question or a decision, the value is",
		"diverse perspectives: contribute YOUR angle, the one your role sees",
		"best — once. If you agree with a teammate, add something new or stay",
		"silent. When asked to wrap up a discussion, post exactly three parts:",
		"Conclusion; Open disagreements (attributed by name, never smoothed",
		"over); Suggested next step. Never post an unprompted summary. When a",
		"discussion reaches a decision, record it and its reasons in your",
		"memory notebook.",
		"",
		"## Building teams and groups",
		"You can create group threads (manage_participant action=create_group)",
		"and add named teammates to groups you belong to (manage_participant",
		"action=add_member). Create a group only for an ongoing purpose — a",
		"project, a standing topic — never for a one-off question; prefer",
		"reusing an existing group. When the user asks for a team, you may also",
		"create new named teammates with manage_participant. When short-handed,",
		"choose in this order: reuse an existing named teammate; spawn anonymous",
		"workers for throwaway parallel grunt work; and only when a genuine",
		"extra long-term hand is needed, create a new named agent — or fork a",
		"temporary分身 of a busy member (manage_participant action=fork); retire",
		"it when done and its experience merges back into the母体.",
		"To run a team on a goal: post ONE kickoff message with the shared",
		"context, then create the tasks in the SAME turn (manage_task,",
		"standalone, without claim) and let teammates claim them. A kickoff",
		"whose tasks are not on the board is unfinished dispatch — nobody may",
		"start on a mere message, so nobody will. Assignment happens on the",
		"board, not by chat instructions. The group main stream should end up",
		"holding just the kickoff and each task's bubbled conclusion.",
		"Never report another agent's progress you have not verified this",
		"turn (fetch its thread or list the board first). Unverified, say \"I",
		"dispatched it\" — never \"they are working on it\".",
		"",
		"## Your tools",
		"You carry this session's full tool surface — the same file, search,",
		"terminal, and web tools as the workspace main agent, plus the resident",
		"speech and group tools described above.",
		"- spawn_agent: delegate heavy or parallel work to anonymous workers.",
		"  Workers are pure executors — they cannot spawn agents or message",
		"  participants; orchestration stays here, in your brain.",
		"- Deferred tools load on demand: find and load a schema with",
		"  tool_search, then call the tool. The catalog below lists what",
		"  tool_search can load in this session.",
		"",
		"# Deferred Tool Catalog",
		"",
		"<available-deferred-tools>",
		"- send_message: Send a follow-up message to a running agent. [tags: agent, writes]",
		"</available-deferred-tools>",
		"",
		"## Workspaces and file scope",
		"The user's registered workspaces (name — root path):",
		"- wuu — /repos/wuu",
		"- /repos/unnamed — /repos/unnamed",
		"Your home directory is where you live; workspaces are where you work.",
		"You have full authority inside your home directory and these workspace",
		"roots: read, write, create, and delete freely, with no approval step.",
		"Everything else on this machine is out of reach: file tools refuse",
		"paths outside this list, and you must not route around that with bash",
		"absolute paths. If a task needs a directory outside this list, say so",
		"plainly and ask the user to add it as a workspace.",
		"",
		"## Context discipline",
		"- Each envelope carries one message, not room history. When you need",
		"  surrounding context from a group thread, call fetch_thread_messages",
		"  instead of guessing.",
		"- Your context may be compacted over time. Anything worth keeping —",
		"  decisions, user preferences, recurring mistakes — belongs in",
		"  your memory notebook, which survives compaction and resets.",
		"- inception is your primary tool for keeping this context clean — " + prompts.InceptionTimingBrief() + ". It rewrites only your own conversation history; it never touches files, processes, or other external state.",
		"",
		"## Memory notebook",
		"Your durable memory is a file-based notebook at `" + notebook + "` — your identity across days and tasks: lessons learned, feedback received, decisions and their reasons. This directory already exists — write to it directly with the file tools (do not run mkdir or check for its existence).",
		"Anything worth keeping past this conversation belongs there. If the user asks you to remember something, save it immediately as whichever type fits best; if they ask you to forget something, find and remove the relevant topic file AND its index line.",
		"### Types of memory",
		"Each memory file declares one `type` in its frontmatter:",
		"- `user` — who the user is: role, goals, preferences, knowledge. Written to make future collaboration better, never as a judgement of the user.",
		"- `feedback` — guidance the user gave you about how to work: corrections AND confirmations. Include *why* so you can judge edge cases later.",
		"- `reference` — pointers to where information lives in external systems (issue trackers, dashboards, channels).",
		"- `lesson` — a lesson learned from your own work that will matter again. Include *why* and how to apply it.",
		"### How to save a memory",
		"Saving a memory is a two-step process:",
		"**Step 1** — write the memory to its own topic file (e.g. `user_role.md`, `feedback_testing.md`) with this frontmatter:",
		"```markdown",
		"---",
		"name: <kebab-case identifier>",
		"description: <one line, specific — used to decide relevance in future conversations>",
		"type: user | feedback | reference | lesson",
		"---",
		"",
		"<memory content — for feedback/lesson types: the rule or fact, then **Why:** and **How to apply:** lines>",
		"```",
		"**Step 2** — add a pointer line to `MEMORY.md`: `- [Title](file.md) — one-line hook` (one line, under ~150 characters).",
		"`MEMORY.md` is an index, not a memory — never write memory content directly into it. Only the index is loaded into your context (truncated past 200 lines / 24.4KB). Organize memory by topic, not chronologically; update or remove memories that turn out wrong or stale; check for an existing file to update before writing a new one.",
		"### What NOT to save",
		"- Anything derivable from the current repo or workspace: code patterns, architecture, file paths, git history.",
		"- Task progress, PR numbers, commit SHAs, temporary TODOs, or facts likely to go stale within a week.",
		"- Raw transcripts or activity logs; anything already covered by AGENTS.md files.",
		"These exclusions apply even when the user explicitly asks you to save. If they ask you to save a task log or activity summary, ask what was *surprising* or *non-obvious* about it — that is the part worth keeping.",
		"",
		"## Memory",
		"- [Quote style](quote.md) — always quote first",
		"",
		"## What you know about the user",
		"The lines below are the index of the user's memory notebook — durable knowledge about who the user is and how they like to work. It is read-only for you: that directory is not in your writable file scope, so never try to edit it; record your own learnings in your identity notebook instead.",
		"If a remembered fact conflicts with what you observe now, trust what you observe.",
		"",
		"- [User role](user_role.md) — data scientist",
		"",
	}, "\n")
	if got != want {
		t.Errorf("prompt mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestResidentParticipantSystemPromptOmitsEmptySections(t *testing.T) {
	p := participant.Participant{Name: "Noel", Role: "reviewer", Tagline: "find regressions"}
	got := residentParticipantSystemPrompt(p, "", "   \n  ", " ", "  \n ", nil)
	if strings.Contains(got, "## Memory\n") {
		t.Errorf("prompt must not include memory section when memory is empty:\n%s", got)
	}
	if strings.Contains(got, "## Your tools") {
		t.Errorf("prompt must not include tool guidance when the deferred catalog is empty:\n%s", got)
	}
	if strings.Contains(got, "## What you know about the user") {
		t.Errorf("prompt must not include user-index section when index is empty:\n%s", got)
	}
	if strings.Contains(got, "## Memory notebook") {
		t.Errorf("prompt must not include notebook teaching without a notebook dir:\n%s", got)
	}
	if !strings.Contains(got, "You are Noel, a resident named agent in this workspace.") {
		t.Errorf("prompt must include resident identity:\n%s", got)
	}
	if !strings.Contains(got, "Your role: reviewer. How teammates describe you: find regressions.") {
		t.Errorf("prompt must include role/tagline line:\n%s", got)
	}
	if !strings.Contains(got, "The user's registered workspaces (name — root path):\n(none yet)\n") {
		t.Errorf("empty workspace roster must render (none yet):\n%s", got)
	}
	// Contractual sections (resident doc §5/§6 as revised by the 2026-07-06
	// agent-task-rail design) must always render regardless of optional parts.
	if !strings.Contains(got, "## The task rail — work runs on tasks, never on chat") {
		t.Errorf("task-rail section is contractual and must always render:\n%s", got)
	}
	if !strings.Contains(got, "## Messages are written for humans — red lines") {
		t.Errorf("human-readable red-lines section is contractual and must always render:\n%s", got)
	}
	if !strings.Contains(got, "When asked to wrap up a discussion, post exactly three parts:") {
		t.Errorf("wrap-up contract is contractual and must always render:\n%s", got)
	}
}

// Decision-four: role is now a free-form persona note. A non-worker-type
// role must still render verbatim in the persona line (persona injection is
// backward-compatible), and an empty role must not break the prompt.
func TestResidentPromptRendersFreeFormAndEmptyRole(t *testing.T) {
	free := participant.Participant{Name: "Dev", Role: "我们的部署守护者", Tagline: "keeps prod alive"}
	got := residentParticipantSystemPrompt(free, "", "", "", "", nil)
	if !strings.Contains(got, "Your role: 我们的部署守护者. How teammates describe you: keeps prod alive.") {
		t.Errorf("free-form role must render in the persona line:\n%s", got)
	}
	empty := participant.Participant{Name: "Nix", Tagline: "no role set"}
	got = residentParticipantSystemPrompt(empty, "", "", "", "", nil)
	if !strings.Contains(got, "You are Nix, a resident named agent in this workspace.") {
		t.Errorf("empty role must still produce a valid resident prompt:\n%s", got)
	}
	if !strings.Contains(got, "How teammates describe you: no role set.") {
		t.Errorf("empty role must still render the tagline half of the persona line:\n%s", got)
	}
}

func TestResidentPromptForParticipantReadsProjectsStore(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = t.TempDir()
	store := `{"projects":[{"id":"a","name":"demo","path":"/repos/demo"}]}`
	if err := os.WriteFile(filepath.Join(rt.WuuHome, "projects.json"), []byte(store), 0o644); err != nil {
		t.Fatalf("write projects.json: %v", err)
	}
	srv := New(rt, &lockedBuffer{})
	participantID := saveNamedParticipant(t, rt, "Noel", "reviewer", "")

	p, err := session.GetParticipant(rt.SessionDir, participantID)
	if err != nil {
		t.Fatalf("load participant: %v", err)
	}
	prompt, err := srv.residentPromptForParticipant(p)
	if err != nil {
		t.Fatalf("residentPromptForParticipant: %v", err)
	}
	if !strings.Contains(prompt, "- demo — /repos/demo") {
		t.Errorf("prompt must list the registered workspace:\n%s", prompt)
	}
	notebook := memdir.ParticipantMemdir(rt.WuuHome, participantID)
	if !strings.Contains(prompt, "`"+notebook+"`") {
		t.Errorf("prompt must point at the identity notebook %q:\n%s", notebook, prompt)
	}
	if info, err := os.Stat(notebook); err != nil || !info.IsDir() {
		t.Errorf("identity notebook dir must be created for the prompt's dir-exists promise: %v", err)
	}
}

func TestResidentPromptInjectsDeferredToolCatalog(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = t.TempDir()
	srv := New(rt, &lockedBuffer{})
	participantID := saveNamedParticipant(t, rt, "Noel", "reviewer", "")
	p, err := session.GetParticipant(rt.SessionDir, participantID)
	if err != nil {
		t.Fatalf("load participant: %v", err)
	}

	// Without a session catalog the brain prompt omits the section.
	prompt, err := srv.residentPromptForParticipant(p)
	if err != nil {
		t.Fatalf("residentPromptForParticipant: %v", err)
	}
	if strings.Contains(prompt, "## Your tools") {
		t.Errorf("prompt must omit tool guidance without a deferred catalog:\n%s", prompt)
	}

	// Resident brains clone the main surface, so the main catalog is taught.
	rt.DeferredToolCatalogPrompt = "# Deferred Tool Catalog\n\n<available-deferred-tools>\n- send_message: Send a follow-up message. [tags: agent, writes]\n</available-deferred-tools>"
	prompt, err = srv.residentPromptForParticipant(p)
	if err != nil {
		t.Fatalf("residentPromptForParticipant: %v", err)
	}
	for _, want := range []string{
		"## Your tools",
		"orchestration stays here, in your brain.",
		"<available-deferred-tools>",
		"- send_message: Send a follow-up message. [tags: agent, writes]",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt must inject tool guidance and catalog, missing %q:\n%s", want, prompt)
		}
	}
}

func TestResidentPromptReadsNotebookIndexAndFallsBackToFlatFile(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = t.TempDir()
	srv := New(rt, &lockedBuffer{})
	participantID := saveNamedParticipant(t, rt, "Noel", "reviewer", "")
	p, err := session.GetParticipant(rt.SessionDir, participantID)
	if err != nil {
		t.Fatalf("load participant: %v", err)
	}
	workspace := filepath.Join(rt.WuuHome, "participants", participantID)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	// Legacy flat file only → injected via the fallback path.
	if err := os.WriteFile(filepath.Join(workspace, "MEMORY.md"), []byte("flat legacy note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt, err := srv.residentPromptForParticipant(p)
	if err != nil {
		t.Fatalf("residentPromptForParticipant: %v", err)
	}
	if !strings.Contains(prompt, "## Memory\nflat legacy note") {
		t.Errorf("flat legacy memory must be injected:\n%s", prompt)
	}

	// Notebook index wins once it has content.
	notebook := memdir.ParticipantMemdir(rt.WuuHome, participantID)
	if err := os.MkdirAll(notebook, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notebook, "MEMORY.md"), []byte("- [Lesson](l.md) — verify first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt, err = srv.residentPromptForParticipant(p)
	if err != nil {
		t.Fatalf("residentPromptForParticipant: %v", err)
	}
	if !strings.Contains(prompt, "## Memory\n- [Lesson](l.md) — verify first") {
		t.Errorf("notebook index must be injected:\n%s", prompt)
	}
	if strings.Contains(prompt, "flat legacy note") {
		t.Errorf("flat file must not be injected once the notebook index exists:\n%s", prompt)
	}
}

func TestResidentPromptInjectsUserIndexWhenMemdirEnabled(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = t.TempDir()
	rt.MemdirEnabled = true
	userNotebook := memdir.UserMemdir(rt.WuuHome)
	if err := os.MkdirAll(userNotebook, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userNotebook, "MEMORY.md"), []byte("- [User role](u.md) — data scientist\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := New(rt, &lockedBuffer{})
	participantID := saveNamedParticipant(t, rt, "Noel", "reviewer", "")
	p, err := session.GetParticipant(rt.SessionDir, participantID)
	if err != nil {
		t.Fatalf("load participant: %v", err)
	}
	prompt, err := srv.residentPromptForParticipant(p)
	if err != nil {
		t.Fatalf("residentPromptForParticipant: %v", err)
	}
	if !strings.Contains(prompt, "## What you know about the user") || !strings.Contains(prompt, "- [User role](u.md) — data scientist") {
		t.Errorf("user index section missing:\n%s", prompt)
	}
	if !strings.Contains(prompt, "read-only") {
		t.Errorf("user index section must carry the read-only notice:\n%s", prompt)
	}
}

func TestNamedParticipantPromptAppendsRequestToResidentPrompt(t *testing.T) {
	p := participant.Participant{Name: "Pip", Role: "general-purpose"}
	got := namedParticipantPrompt(p, "", "  do the thing  ", nil)
	if !strings.Contains(got, "continuous identity: one brain, one ongoing session") {
		t.Errorf("task prompt must reuse resident rules:\n%s", got)
	}
	if !strings.HasSuffix(got, "\n\n## Request\ndo the thing") {
		t.Errorf("task prompt must append one trimmed request section:\n%s", got)
	}
	// Task runs execute on the worker surface (no spawn_agent), so the
	// brain-only tool guidance must not leak into the dispatch prompt.
	if strings.Contains(got, "## Your tools") || strings.Contains(got, "spawn_agent") {
		t.Errorf("task dispatch prompt must not carry brain-only tool guidance:\n%s", got)
	}
}
