package appserver

import (
	"strings"
	"testing"
	"time"
)

func TestMessageEnvelopePromptRendersCompactAttributes(t *testing.T) {
	env := MessageEnvelope{
		ID:             "env-1",
		SourceThreadID: "thread-1",
		SourceTitle:    `Design "Room"`,
		SenderKind:     "user",
		SenderName:     `Ada "A"`,
		Addressed:      true,
		Hop:            1,
		Text:           "  please review\n\nthis  ",
		CreatedAt:      time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC),
	}

	got := env.Prompt()
	want := "<incoming_message thread=\"Design \\\"Room\\\"\" thread_id=\"thread-1\" from=\"user\" sender=\"Ada \\\"A\\\"\" addressed=\"true\" hop=\"1\">\nplease review\n\nthis\n</incoming_message>"
	if got != want {
		t.Fatalf("Prompt() = %q, want %q", got, want)
	}
}

func TestCoalesceEnvelopesAddsBusyBatchHeader(t *testing.T) {
	envs := []MessageEnvelope{
		{SourceThreadID: "thread-a", SourceTitle: "A", SenderKind: "user", SenderName: "User", Text: "first"},
		{SourceThreadID: "thread-b", SourceTitle: "B", SenderKind: "participant", SenderName: "Noel", Addressed: true, Hop: 1, Text: "second"},
	}

	got := coalesceEnvelopes(envs)
	if !strings.HasPrefix(got, "You received 2 messages while busy:\n\n") {
		t.Fatalf("coalesced prompt missing busy header: %q", got)
	}
	if strings.Count(got, "<incoming_message") != 2 {
		t.Fatalf("coalesced prompt should contain two envelopes: %q", got)
	}
	if !strings.Contains(got, `thread_id="thread-a"`) || !strings.Contains(got, `thread_id="thread-b"`) {
		t.Fatalf("coalesced prompt missing source thread IDs: %q", got)
	}
}

func TestCoalesceEnvelopesLeavesSingleEnvelopeUnprefixed(t *testing.T) {
	got := coalesceEnvelopes([]MessageEnvelope{{SourceThreadID: "thread-a", SourceTitle: "A", SenderKind: "user", SenderName: "User", Text: "first"}})
	if strings.HasPrefix(got, "You received") {
		t.Fatalf("single envelope should not get busy header: %q", got)
	}
	if strings.Count(got, "<incoming_message") != 1 {
		t.Fatalf("single envelope prompt missing envelope: %q", got)
	}
}

// TestMessageEnvelopePromptWorkspaceAttribute covers the workspace
// attribute added by 2026-07-03-workspace-focus.md "carry source-thread
// workspace focus on envelopes": "" (no focus declared) omits the
// attribute entirely, "~" (home) renders as workspace="home", and any
// other value is a registered workspace name rendered verbatim.
func TestMessageEnvelopePromptWorkspaceAttribute(t *testing.T) {
	base := MessageEnvelope{
		SourceThreadID: "thread-1",
		SourceTitle:    "Room",
		SenderKind:     "user",
		SenderName:     "User",
		Text:           "hi",
	}

	for _, tc := range []struct {
		name      string
		workspace string
		wantAttr  string // "" means the attribute must be absent
	}{
		{"no focus declared", "", ""},
		{"home", "~", `workspace="home"`},
		{"named workspace", "acme", `workspace="acme"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := base
			env.Workspace = tc.workspace
			got := env.Prompt()
			if tc.wantAttr == "" {
				if strings.Contains(got, "workspace=") {
					t.Fatalf("expected no workspace attribute, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantAttr) {
				t.Fatalf("Prompt() = %q, want it to contain %q", got, tc.wantAttr)
			}
		})
	}
}
