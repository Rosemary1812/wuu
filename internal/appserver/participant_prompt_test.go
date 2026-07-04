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
	got := residentParticipantSystemPrompt(p, notebook, "- [Quote style](quote.md) — always quote first", "- [User role](user_role.md) — data scientist", []workspaces.Workspace{
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
		"and a hop count (how many agent-to-agent relays preceded it). Several",
		"blocks may arrive in one batch if they came in while you were working —",
		"read the whole batch before responding to any of it.",
		"Messages in this conversation without an <incoming_message> wrapper are",
		"the user speaking to you directly (DM). DMs are always addressed to you.",
		"",
		"## Whether and where to reply — your judgment, plus two hard rules",
		"1. Addressed messages (DM or @mention) MUST be answered: either reply",
		"   with substance, or call decline with a one-line reason. Never end a",
		"   turn silently on an addressed message.",
		"2. Unaddressed group messages: reply ONLY when you add real value —",
		"   a correction, a blocker you noticed, information others lack.",
		"   Silence is a valid outcome; simply do not post. Never acknowledge,",
		"   echo, or \"+1\".",
		"",
		"## How to reply",
		"- To a DM: call post_message (omit thread_id — it defaults to this DM).",
		"  Plain assistant text is your private working transcript; the chat view",
		"  renders only tool-posted messages, so text outside post_message never",
		"  reaches the user.",
		"- To a group thread: call post_message with thread_id set to the source",
		"  thread. Keep group replies short and substantive.",
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
	got := residentParticipantSystemPrompt(p, "", "   \n  ", " ", nil)
	if strings.Contains(got, "## Memory\n") {
		t.Errorf("prompt must not include memory section when memory is empty:\n%s", got)
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
}
