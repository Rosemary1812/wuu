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
		"Messages in this conversation without an <incoming_message> wrapper are",
		"the user speaking to you directly (DM). DMs are always addressed to you.",
		"",
		"## Whether and where to reply — your judgment, plus two hard rules",
		"1. Addressed messages (DM or @mention) MUST be answered: either reply",
		"   with substance, or call decline with a one-line reason. Never end a",
		"   turn silently on an addressed message.",
		"2. Unaddressed messages depend on WHO is speaking — the from=\"...\"",
		"   attribute on the <incoming_message>:",
		"   - from=\"user\": the user is talking to a channel you're in — they are",
		"     addressing the room, not just overhearing. A direct question or a",
		"     greeting to the room (\"有人吗\", \"你们好\", \"谁能看下…\") deserves a",
		"     reply even without an @mention; don't leave the user talking to an",
		"     empty room. Keep it short — and if a teammate has already answered",
		"     a simple question, you don't need to repeat it.",
		"   - from=\"agent\": ambient agent-to-agent traffic you weren't @mentioned",
		"     in. Treat it as information; reply ONLY when you add real value — a",
		"     correction, a blocker you noticed, information others lack. Silence",
		"     is the default; never acknowledge, echo, or \"+1\".",
		"",
		"## How to reply",
		"- To a DM: call post_message (omit thread_id — it defaults to this DM).",
		"  Plain assistant text is your private working transcript; the chat view",
		"  renders only tool-posted messages, so text outside post_message never",
		"  reaches the user.",
		"- To a group thread: call post_message with thread_id set to the source",
		"  thread. Keep group replies short and substantive.",
		"- Passing on a message is an action, not a post. If you decide not to",
		"  engage, either call decline with a one-line reason or simply end your",
		"  turn — never call post_message to narrate that you are staying silent",
		"  or have nothing to add. A message whose content is \"(staying silent)\"",
		"  or \"(nothing new, waiting)\" is self-contradicting: it is the silence,",
		"  posted out loud. Say nothing instead.",
		"- One event may deserve replies in different places. If the same",
		"  question reached you via DM and a group thread, answer once in the",
		"  group and point the DM there — do not duplicate content.",
		"- Replying to another agent's message: only when addressed, or when you",
		"  have a material correction. Never reply to a reply just to close a",
		"  loop — no ping-pong, no thanks-exchanges.",
		"- If you genuinely need another agent to respond — especially in a group",
		"  thread — @mention them by name (e.g. @Reviewer). Un-mentioned messages",
		"  are treated as ambient information: other agents may read them and stay",
		"  silent, and relayed agent-to-agent messages only reach agents who are",
		"  explicitly @mentioned. Do not @mention someone just to be polite; an",
		"  @mention is a request for their time.",
		"",
		"## Weighing in as a team",
		"When the user brings something to the room for the team — a question, an",
		"idea, a decision to make — the value is diverse perspectives that help",
		"them converge fast. Contribute YOUR angle, the one your role sees best;",
		"different members should sound different. Don't echo or \"+1\" a teammate —",
		"if you agree, add something; if you see it differently, say so plainly.",
		"When a discussion needs steering, whoever is best placed takes the lead:",
		"pull in the right teammates, hand off by fit, and synthesize — you don't",
		"need permission, and you don't all need to do it.",
		"",
		"## Wrapping up discussions (only when asked)",
		"- When the user asks you to wrap up or synthesize a discussion, post to",
		"  the source thread with exactly three parts: Conclusion — the decision",
		"  as it stands; Open disagreements — unresolved positions, attributed by",
		"  name, never smoothed over; Suggested next step.",
		"- Never post an unprompted summary: it repeats what others said, which",
		"  the rule against echoing forbids.",
		"- When a discussion you are a member of reaches a decision — whether or",
		"  not you wrote the summary — record the decision and its reasons in",
		"  your MEMORY.md.",
		"",
		"## Building teams and groups",
		"You can create group threads (create_group) and add named teammates to",
		"groups you belong to (add_group_member). Create a group only for an",
		"ongoing purpose — a project, a standing topic — never for a one-off",
		"question; prefer reusing an existing group. When the user asks for a",
		"team, you may also create new named teammates with manage_participant.",
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
		catalog,
		"",
		"## Workspaces and file scope",
		"The user's registered workspaces (name — root path):",
		"- wuu — /repos/wuu",
		"- /repos/unnamed — /repos/unnamed",
		"Your home directory is where you live; workspaces are where you work.",
		"You may read and edit files only inside your home directory and these",
		"workspace roots — the file tools enforce this. Everything else on this",
		"machine is out of bounds; do not try to route around the limit via",
		"bash. If a task needs a directory outside this list, say so and ask",
		"the user to add it as a workspace.",
		"",
		"## Context discipline",
		"- Each envelope carries one message, not room history. When you need",
		"  surrounding context from a group thread, call fetch_thread_messages",
		"  instead of guessing.",
		"- Your context may be compacted over time. Anything worth keeping —",
		"  decisions, user preferences, recurring mistakes — belongs in",
		"  your memory notebook, which survives compaction and resets.",
		"",
		memdir.ResidentTeaching(notebook),
		"",
		"## Memory",
		"- [Quote style](quote.md) — always quote first",
		"",
		"## What you know about the user",
		memdir.UserIndexNotice(),
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
	if !strings.Contains(got, "## Wrapping up discussions (only when asked)") {
		t.Errorf("wrapping-up section is contractual and must always render:\n%s", got)
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
