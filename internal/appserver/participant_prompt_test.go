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
	for _, want := range []string{
		"## Group -> Thread -> Task",
		"action=open_thread",
		"No standalone Thread or direct Task",
		"only the Thread owner may call manage_task action=promote",
		"The Lead never takes a piece",
		"add_piece, revise_piece",
		"attempt history are durable",
		"awaiting_lead",
		"action=conclude",
		"have Threads, Tasks, or a board. The cycle",
		"# Deferred Tool Catalog",
		"- wuu — /repos/wuu",
		"## Memory notebook",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{
		"action=claim", "action=unclaim", "action=create one",
		"action=escalate", "action=update_status", "claim=true",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("prompt still exposes removed workflow %q:\n%s", forbidden, got)
		}
	}
}

func TestResidentParticipantSystemPromptRequiresGroupReplyPostMessage(t *testing.T) {
	got := residentParticipantSystemPrompt(participant.Participant{
		Name:    "Ari",
		Role:    "teammate",
		Tagline: "helps in group chat",
	}, "", "", "", "", nil)

	for _, want := range []string{
		"Group replies are visible only through post_message.",
		"If you decide to speak in response to a group <incoming_message>, call post_message",
		"with thread_id set to that incoming_message's source thread_id.",
		"Plain assistant text is private working transcript and never reaches the group.",
		"A group main-stream post is a BRIEF coordination signal, not a report.",
		"Default to kind=brief and stay within 280 characters",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("resident prompt missing group reply contract %q:\n%s", want, got)
		}
	}
}

func TestResidentParticipantSystemPromptCarriesConvergedDMIntoGroup(t *testing.T) {
	got := residentParticipantSystemPrompt(participant.Participant{
		Name:    "Ari",
		Role:    "teammate",
		Tagline: "helps in group chat",
	}, "", "", "", "", nil)

	for _, want := range []string{
		"it may receive a direction already converged in a DM",
		"do not reopen settled decisions without new evidence",
		"A common path starts in your DM: investigate with the user, then bring in a",
		"@ each teammate who should start now",
		"adding them does not assign work or wake them immediately",
		"open a Thread from that kickoff and converge only remaining",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("resident prompt missing DM-to-group handoff contract %q:\n%s", want, got)
		}
	}
}

func TestResidentParticipantSystemPromptRequiresSemanticTaskUpdates(t *testing.T) {
	got := residentParticipantSystemPrompt(participant.Participant{Name: "Ari"}, "", "", "", "", nil)
	for _, want := range []string{
		"Do not\n   narrate tool activity",
		"when a phase completes",
		"answer to 'where is this now?' materially changes",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("resident prompt missing task progress contract %q:\n%s", want, got)
		}
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
	// Group workflow and human-readable messaging rules always render.
	if !strings.Contains(got, "## Group -> Thread -> Task") {
		t.Errorf("group workflow section is contractual and must always render:\n%s", got)
	}
	if !strings.Contains(got, "## Messages are written for humans — red lines") {
		t.Errorf("human-readable red-lines section is contractual and must always render:\n%s", got)
	}
	if !strings.Contains(got, "When asked to wrap up a discussion, post exactly three parts:") {
		t.Errorf("wrap-up contract is contractual and must always render:\n%s", got)
	}
}

func TestResidentParticipantSystemPromptGuidesDelayedUnreadBatches(t *testing.T) {
	p := participant.Participant{Name: "Noel", Role: "reviewer", Tagline: "find regressions"}
	got := residentParticipantSystemPrompt(p, "", "", "", "", nil)
	for _, want := range []string{
		"Delayed unread messages are a chance to catch up, not a summons",
		"If a delayed batch contains several messages or changes your view of the room, consider inception",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing delayed-unread guidance %q:\n%s", want, got)
		}
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
