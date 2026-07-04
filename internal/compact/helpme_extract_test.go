package compact

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// journalRecordingClient is a plain (non-streaming) client that records every
// extraction prompt and replays canned responses per call, last one repeating.
type journalRecordingClient struct {
	mu        sync.Mutex
	prompts   []string
	responses []string
}

func (c *journalRecordingClient) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prompt := ""
	if len(req.Messages) > 0 {
		prompt = req.Messages[len(req.Messages)-1].Content
	}
	c.prompts = append(c.prompts, prompt)
	idx := len(c.prompts) - 1
	if idx >= len(c.responses) {
		idx = len(c.responses) - 1
	}
	return providers.ChatResponse{Content: c.responses[idx]}, nil
}

func (c *journalRecordingClient) recorded() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.prompts...)
}

func TestBuildHelpMeParentJournalSingleChunk(t *testing.T) {
	client := &journalRecordingClient{responses: []string{"### User goal\n\"fix the flaky login test\"\n\n### Paths taken\n- patched the router guard - ruled out"}}
	history := []providers.ChatMessage{
		{Role: "system", Content: "system prompt that must not be extracted"},
		{Role: "user", Content: "fix the flaky login test"},
		{Role: "assistant", Content: "I will patch the router guard"},
	}

	journal, err := BuildHelpMeParentJournal(context.Background(), client, "fake-model", Budget{}, history)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(journal, "ruled out") || !strings.Contains(journal, "fix the flaky login test") {
		t.Fatalf("journal lost canned extraction content:\n%s", journal)
	}
	prompts := client.recorded()
	if len(prompts) != 1 {
		t.Fatalf("expected one extraction call for a short history, got %d", len(prompts))
	}
	for _, want := range []string{"decision journal", "ruled out", "Transcript to extract from", "fix the flaky login test"} {
		if !strings.Contains(prompts[0], want) {
			t.Fatalf("extraction prompt missing %q:\n%s", want, prompts[0])
		}
	}
	if strings.Contains(prompts[0], "Previous journal") {
		t.Fatalf("single-chunk extraction must not carry a previous journal marker:\n%s", prompts[0])
	}
	// The extraction prompt is not the continuation-style compact prompt.
	if strings.Contains(prompts[0], "context compaction") || strings.Contains(prompts[0], "## External State") {
		t.Fatalf("extraction must not reuse the continuation summary prompt:\n%s", prompts[0])
	}
}

// TestBuildHelpMeParentJournalRollsOverChunks locks the map-reduce shape for
// long histories: the transcript is split into budget-sized chunks, each call
// after the first carries the rolling journal, and the final journal is the
// last round's output.
func TestBuildHelpMeParentJournalRollsOverChunks(t *testing.T) {
	responses := []string{"journal v1: guard path ruled out", "journal v2: keeps v1 facts", "journal v3: final"}
	client := &journalRecordingClient{responses: responses}

	history := make([]providers.ChatMessage, 0, 41)
	history = append(history, providers.ChatMessage{Role: "system", Content: "sys"})
	filler := strings.Repeat("very long transcript line about the login investigation. ", 12)
	for i := 0; i < 40; i++ {
		history = append(history, providers.ChatMessage{Role: "assistant", Content: fmt.Sprintf("step %d: %s", i, filler)})
	}

	// A tight input budget forces multiple chunks (each rendered message is
	// capped at compactPromptContentMaxChars, so ~130 tokens per message).
	budget := Budget{ContextTokens: 1200, InputTokens: 1200}
	journal, err := BuildHelpMeParentJournal(context.Background(), client, "fake-model", budget, history)
	if err != nil {
		t.Fatal(err)
	}
	prompts := client.recorded()
	if len(prompts) < 2 {
		t.Fatalf("expected chunked extraction to need multiple calls, got %d", len(prompts))
	}
	if strings.Contains(prompts[0], "--- Previous journal ---") {
		t.Fatalf("first chunk must not carry a previous journal:\n%s", prompts[0])
	}
	if !strings.Contains(prompts[1], "--- Previous journal ---") || !strings.Contains(prompts[1], "journal v1: guard path ruled out") {
		t.Fatalf("second chunk must roll the previous journal forward:\n%s", prompts[1])
	}
	wantIdx := len(prompts) - 1
	if wantIdx >= len(responses) {
		wantIdx = len(responses) - 1
	}
	wantFinal := responses[wantIdx]
	if journal != wantFinal {
		t.Fatalf("journal should be the last rolling output %q, got %q", wantFinal, journal)
	}
}

func TestBuildHelpMeJointCompactContentRendersParentJournalFirst(t *testing.T) {
	content := BuildHelpMeJointCompactContent(HelpMeJointCompactInput{
		OriginalGoal:           "fix the broken login test",
		ParentExecutionJournal: "### Paths taken\n- patched the router guard - ruled out",
		Reason:                 "main agent tried the wrong auth path twice",
		CurrentUnderstanding:   "self-reported framing",
		ReportSummary:          "real bug was token refresh ordering",
	})
	journalIdx := strings.Index(content, "## Parent execution journal (machine-extracted)")
	supplementaryIdx := strings.Index(content, "## Parent self-reported brief (supplementary)")
	reasonIdx := strings.Index(content, "## Parent rescue reason")
	if journalIdx < 0 || supplementaryIdx < 0 || reasonIdx < 0 {
		t.Fatalf("joint compact missing journal or supplementary sections:\n%s", content)
	}
	if !(journalIdx < supplementaryIdx && supplementaryIdx < reasonIdx) {
		t.Fatalf("journal must render before the demoted self-report (journal=%d supplementary=%d reason=%d):\n%s", journalIdx, supplementaryIdx, reasonIdx, content)
	}
	if !strings.Contains(content, "patched the router guard - ruled out") {
		t.Fatalf("journal content lost:\n%s", content)
	}
}

func TestBuildHelpMeJointCompactContentWithoutJournalKeepsCurrentShape(t *testing.T) {
	content := BuildHelpMeJointCompactContent(HelpMeJointCompactInput{
		OriginalGoal: "fix the broken login test",
		Reason:       "main agent tried the wrong auth path twice",
	})
	for _, unwanted := range []string{"Parent execution journal", "supplementary"} {
		if strings.Contains(content, unwanted) {
			t.Fatalf("journal sections must not render without a journal:\n%s", content)
		}
	}
}
