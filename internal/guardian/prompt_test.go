package guardian

import (
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/tools"
)

func TestTruncateString(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want func(t *testing.T, got string)
	}{
		{
			name: "empty",
			in:   "",
			max:  100,
			want: func(t *testing.T, got string) {
				if got != "" {
					t.Fatalf("got %q, want empty", got)
				}
			},
		},
		{
			name: "under max returns as-is",
			in:   "hello",
			max:  100,
			want: func(t *testing.T, got string) {
				if got != "hello" {
					t.Fatalf("got %q, want hello", got)
				}
			},
		},
		{
			name: "exactly max returns as-is",
			in:   "hello",
			max:  5,
			want: func(t *testing.T, got string) {
				if got != "hello" {
					t.Fatalf("got %q, want hello", got)
				}
			},
		},
		{
			name: "over max with marker room",
			in:   strings.Repeat("a", 200),
			max:  40,
			want: func(t *testing.T, got string) {
				if len(got) != 40 {
					t.Fatalf("len=%d, want 40", len(got))
				}
				if !strings.HasSuffix(got, truncationMarker) {
					t.Fatalf("expected truncation marker, got %q", got)
				}
			},
		},
		{
			name: "over max without marker room truncates without marker",
			in:   "hello world",
			max:  3,
			want: func(t *testing.T, got string) {
				if len(got) != 3 {
					t.Fatalf("len=%d, want 3", len(got))
				}
				if got != "hel" {
					t.Fatalf("got %q, want hel", got)
				}
			},
		},
		{
			name: "non-positive max returns empty",
			in:   "hello",
			max:  0,
			want: func(t *testing.T, got string) {
				if got != "" {
					t.Fatalf("got %q, want empty", got)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateString(tc.in, tc.max)
			tc.want(t, got)
		})
	}
}

func TestTruncateTranscript_Empty(t *testing.T) {
	got := truncateTranscript(Transcript{})
	if got.Entries == nil {
		t.Fatal("Entries should be non-nil even when empty")
	}
	if len(got.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(got.Entries))
	}
}

func TestTruncateTranscript_UnderCapReturnsAsIs(t *testing.T) {
	in := Transcript{Entries: []TranscriptEntry{
		{Role: TranscriptRoleUser, Content: "hi"},
		{Role: TranscriptRoleAssistant, Content: "hello"},
	}}
	got := truncateTranscript(in)
	if len(got.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got.Entries))
	}
	if got.Entries[0].Content != "hi" || got.Entries[1].Content != "hello" {
		t.Fatalf("entries not preserved: %+v", got.Entries)
	}
}

func TestTruncateTranscript_CapsEntryCount(t *testing.T) {
	var entries []TranscriptEntry
	for i := 0; i < MaxTranscriptEntries+10; i++ {
		entries = append(entries, TranscriptEntry{Role: TranscriptRoleUser, Content: "x"})
	}
	got := truncateTranscript(Transcript{Entries: entries})
	if len(got.Entries) != MaxTranscriptEntries {
		t.Fatalf("expected %d entries, got %d", MaxTranscriptEntries, len(got.Entries))
	}
	// Must keep the LAST entries, not the first.
	if got.Entries[0].Content != "x" {
		t.Fatalf("first kept entry should still be x; got %q", got.Entries[0].Content)
	}
}

func TestTruncateTranscript_TruncatesEachEntry(t *testing.T) {
	long := strings.Repeat("a", MaxEntryChars+500)
	got := truncateTranscript(Transcript{Entries: []TranscriptEntry{
		{Role: TranscriptRoleUser, Content: long},
	}})
	if len(got.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got.Entries))
	}
	if len(got.Entries[0].Content) > MaxEntryChars {
		t.Fatalf("entry content len=%d exceeds cap %d", len(got.Entries[0].Content), MaxEntryChars)
	}
}

func TestTruncateTranscript_DropsOldestForTotalCap(t *testing.T) {
	// 12 entries of 5000 chars each = 60000 chars, well over the 40000 cap.
	var entries []TranscriptEntry
	for i := 0; i < 12; i++ {
		entries = append(entries, TranscriptEntry{Role: TranscriptRoleUser, Content: strings.Repeat("x", 5000)})
	}
	got := truncateTranscript(Transcript{Entries: entries})
	total := 0
	for _, e := range got.Entries {
		total += len(e.Content)
	}
	if total > MaxTranscriptChars {
		t.Fatalf("total chars=%d exceeds cap %d", total, MaxTranscriptChars)
	}
	if len(got.Entries) == 0 {
		t.Fatal("must keep at least one entry even when over budget")
	}
}

func TestBuildPrompt_RendersToolFields(t *testing.T) {
	req := tools.ToolApprovalReviewRequest{
		ToolName:             "run_shell",
		Kind:                 tools.ToolKindShell,
		Risk:                 tools.ToolRiskHigh,
		ReadOnly:             false,
		Destructive:          true,
		PolicyReason:         "shell is high risk",
		ClassificationReason: "executes external binaries",
		Capability:           capability.CapabilityCommandBash,
		CapabilityObject:     "git commit -m test",
		CapabilityAction:     "execute",
		CapabilityRule:       "bash-git-commit",
		Permission:           "command.bash",
		PermissionPatterns:   []string{"git commit *"},
		PermissionAlways:     []string{"git commit *"},
		PermissionRule:       "command.bash git commit *",
		ModelNextAction:      "choose a lower-risk alternative",
		ArgumentsPreview:     `{"command": "rm -rf build"}`,
	}
	out, err := BuildPrompt(req, Transcript{})
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	wantSubstrings := []string{
		"approve for me",
		"run_shell",
		"shell",
		"high",
		"Read-only: no",
		"Destructive: yes",
		"shell is high risk",
		"executes external binaries",
		"Capability under review",
		"command.bash",
		"git commit -m test",
		"Permission request under review",
		"git commit *",
		"choose a lower-risk alternative",
		"rm -rf build",
		"Decision framework",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(out, want) {
			t.Fatalf("prompt missing %q\n--- prompt ---\n%s", want, out)
		}
	}
}

func TestBuildPrompt_UsesRiskFrameworkInsteadOfHardPathBlocks(t *testing.T) {
	req := tools.ToolApprovalReviewRequest{ToolName: "x", Kind: tools.ToolKindShell, Risk: tools.ToolRiskHigh}
	out, err := BuildPrompt(req, Transcript{})
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	wantSubstrings := []string{
		"Do not deny solely because",
		"specific local path outside",
		"touches .git metadata",
		"uses a powerful primitive such as rm -rf",
		"Use the pre-classified risk as a hint, not as a floor",
		"keep, lower, or raise the risk level",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(out, want) {
			t.Fatalf("prompt missing %q\n--- prompt ---\n%s", want, out)
		}
	}
	oldHardBlocks := []string{
		"The action does not write outside the current workspace, touch .git metadata",
		"Writing outside the workspace or to system paths (/etc, /usr, ~/.ssh, etc.).",
		"Never demote to a level below the pre-classified value",
	}
	for _, old := range oldHardBlocks {
		if strings.Contains(out, old) {
			t.Fatalf("prompt still contains old hard block %q\n--- prompt ---\n%s", old, out)
		}
	}
}

func TestBuildPrompt_IncludesTranscriptEntries(t *testing.T) {
	req := tools.ToolApprovalReviewRequest{ToolName: "x", Kind: tools.ToolKindFile, Risk: tools.ToolRiskLow}
	transcript := Transcript{Entries: []TranscriptEntry{
		{Role: TranscriptRoleUser, Content: "please run the tests"},
		{Role: TranscriptRoleAssistant, Content: "running now"},
	}}
	out, err := BuildPrompt(req, transcript)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	for _, want := range []string{"please run the tests", "running now", "[user]", "[assistant]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("prompt missing %q\n--- prompt ---\n%s", want, out)
		}
	}
}

func TestBuildPrompt_NoTranscriptFallsBackToNote(t *testing.T) {
	req := tools.ToolApprovalReviewRequest{ToolName: "x", Kind: tools.ToolKindFile, Risk: tools.ToolRiskLow}
	out, err := BuildPrompt(req, Transcript{})
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	if !strings.Contains(out, "no prior conversation available") {
		t.Fatalf("expected empty-transcript fallback note, got:\n%s", out)
	}
}

func TestBuildPrompt_TruncatesLongArguments(t *testing.T) {
	req := tools.ToolApprovalReviewRequest{
		ToolName:         "x",
		Kind:             tools.ToolKindFile,
		Risk:             tools.ToolRiskLow,
		ArgumentsPreview: strings.Repeat("a", MaxActionChars*3),
	}
	out, err := BuildPrompt(req, Transcript{})
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	// The arguments section should not exceed MaxActionChars + some
	// headroom for the surrounding template text.
	start := strings.Index(out, "Arguments (truncated, render verbatim):")
	end := strings.Index(out, "## Recent conversation")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("prompt missing arguments or transcript headings:\n%s", out)
	}
	argumentsSection := out[start:end]
	if strings.Count(argumentsSection, "a") > MaxActionChars+100 {
		t.Fatalf("arguments not truncated: %d a's in arguments section", strings.Count(argumentsSection, "a"))
	}
}
