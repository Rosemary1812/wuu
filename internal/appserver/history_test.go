package appserver

import (
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/providers"
	sessionstore "github.com/blueberrycongee/wuu/internal/session"
)

func TestRewriteChatHistoryKeepsHelpMeJointCompact(t *testing.T) {
	sessDir := t.TempDir()
	sess, err := sessionstore.CreateWithMetadata(sessDir, "helpme-compact", t.TempDir())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	history := []providers.ChatMessage{
		{Role: "system", Content: compact.HelpMeJointCompactPrefix + "\nRecovered task state"},
		{Role: "assistant", Content: "continued"},
	}

	if err := rewriteChatHistory(sessDir, sess.ID, history); err != nil {
		t.Fatalf("rewrite history: %v", err)
	}
	loaded, err := loadChatMessages(sessDir, sess.ID)
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected HelpMe compact and assistant to persist, got %+v", loaded)
	}
	if loaded[0].Role != "system" || !compact.IsHelpMeJointCompactContent(loaded[0].Content) || !strings.Contains(loaded[0].Content, "Recovered task state") {
		t.Fatalf("expected persisted HelpMe compact system message, got %+v", loaded[0])
	}
}

func TestLoadChatMessagesModelsParticipantRowsAsHiddenContext(t *testing.T) {
	sessDir := t.TempDir()
	sess, err := sessionstore.CreateWithMetadata(sessDir, "participant-history", t.TempDir())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := sessionstore.AppendHistoryRecord(sessDir, sess.ID, sessionstore.HistoryRecord{
		Role:    "user",
		Content: "review this diff",
	}); err != nil {
		t.Fatalf("append user: %v", err)
	}
	if err := sessionstore.AppendHistoryRecord(sessDir, sess.ID, sessionstore.HistoryRecord{
		Role:          "participant",
		Content:       "Found one regression.",
		Name:          "Noel",
		ParticipantID: "prt-reviewer",
		PostKind:      "result",
	}); err != nil {
		t.Fatalf("append participant: %v", err)
	}
	if err := sessionstore.AppendHistoryRecord(sessDir, sess.ID, sessionstore.HistoryRecord{
		Role:    "assistant",
		Content: "I will use that result.",
	}); err != nil {
		t.Fatalf("append assistant: %v", err)
	}

	loaded, err := loadChatMessages(sessDir, sess.ID)
	if err != nil {
		t.Fatalf("load chat messages: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("participant row should be modeled as hidden context: %+v", loaded)
	}
	if loaded[0].Role != "user" || loaded[1].Role != "user" || loaded[2].Role != "assistant" {
		t.Fatalf("unexpected model history: %+v", loaded)
	}
	ctx := loaded[1]
	if !ctx.Hidden || ctx.Name != participantModelContextMessageName || ctx.ParticipantID != "prt-reviewer" || ctx.ParticipantName != "Noel" || ctx.PostKind != "result" {
		t.Fatalf("unexpected participant context metadata: %+v", ctx)
	}
	if !strings.Contains(ctx.Content, "Noel posted a result card") || !strings.Contains(ctx.Content, "Found one regression.") {
		t.Fatalf("participant context missing attribution/content: %q", ctx.Content)
	}
}

func TestLoadChatMessagesSkipsConversationThreadRows(t *testing.T) {
	sessDir := t.TempDir()
	sess, err := sessionstore.CreateWithMetadata(sessDir, "thread-isolation", t.TempDir())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, rec := range []sessionstore.HistoryRecord{
		{Role: "user", Content: "main request"},
		{Role: "assistant", Content: "private investigation", ThreadID: "cth-review"},
		{Role: "participant", Content: "private result", Name: "Noel", ParticipantID: "prt-reviewer", PostKind: "result", ThreadID: "cth-review"},
		{Role: "assistant", Content: "main response"},
	} {
		if err := sessionstore.AppendHistoryRecord(sessDir, sess.ID, rec); err != nil {
			t.Fatalf("append history: %v", err)
		}
	}

	loaded, err := loadChatMessages(sessDir, sess.ID)
	if err != nil {
		t.Fatalf("load chat messages: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("subthread rows should not enter main model history: %+v", loaded)
	}
	if loaded[0].Content != "main request" || loaded[1].Content != "main response" {
		t.Fatalf("unexpected main model history: %+v", loaded)
	}
}

func TestRewriteChatHistoryPreservesParticipantRowsFromModelContext(t *testing.T) {
	sessDir := t.TempDir()
	sess, err := sessionstore.CreateWithMetadata(sessDir, "participant-rewrite", t.TempDir())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, rec := range []sessionstore.HistoryRecord{
		{Role: "user", Content: "review this diff"},
		{Role: "participant", Content: "Found one regression.", Name: "Noel", ParticipantID: "prt-reviewer", PostKind: "result"},
		{Role: "assistant", Content: "I will use that result."},
	} {
		if err := sessionstore.AppendHistoryRecord(sessDir, sess.ID, rec); err != nil {
			t.Fatalf("append history: %v", err)
		}
	}

	loaded, err := loadChatMessages(sessDir, sess.ID)
	if err != nil {
		t.Fatalf("load chat messages: %v", err)
	}
	if err := rewriteChatHistory(sessDir, sess.ID, loaded); err != nil {
		t.Fatalf("rewrite chat history: %v", err)
	}
	persisted, err := loadPersistedMessages(sessDir, sess.ID, false)
	if err != nil {
		t.Fatalf("load persisted messages: %v", err)
	}
	if len(persisted) != 3 {
		t.Fatalf("expected user, participant, assistant rows after rewrite, got %+v", persisted)
	}
	if persisted[1].Role != "participant" || persisted[1].Content != "Found one regression." || persisted[1].Name != "Noel" || persisted[1].ParticipantID != "prt-reviewer" || persisted[1].PostKind != "result" {
		t.Fatalf("participant row not preserved: %+v", persisted[1])
	}
}

func TestRewriteChatHistoryPreservesConversationThreadRows(t *testing.T) {
	sessDir := t.TempDir()
	sess, err := sessionstore.CreateWithMetadata(sessDir, "subthread-rewrite", t.TempDir())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, rec := range []sessionstore.HistoryRecord{
		{Role: "user", Content: "main request"},
		{Role: "assistant", Content: "private investigation", ThreadID: "cth-review"},
		{Role: "participant", Content: "private result", Name: "Noel", ParticipantID: "prt-reviewer", PostKind: "result", ThreadID: "cth-review"},
		{Role: "meta", Content: "token_usage", InputTokens: 1},
	} {
		if err := sessionstore.AppendHistoryRecord(sessDir, sess.ID, rec); err != nil {
			t.Fatalf("append history: %v", err)
		}
	}

	if err := rewriteChatHistory(sessDir, sess.ID, []providers.ChatMessage{{Role: "assistant", Content: "rewritten main"}}); err != nil {
		t.Fatalf("rewrite chat history: %v", err)
	}
	persisted, err := loadPersistedMessages(sessDir, sess.ID, true)
	if err != nil {
		t.Fatalf("load persisted messages: %v", err)
	}
	if len(persisted) != 4 {
		t.Fatalf("expected rewritten main row, preserved thread rows, and meta row, got %+v", persisted)
	}
	if persisted[0].Content != "rewritten main" || persisted[0].ThreadID != "" {
		t.Fatalf("main row not rewritten correctly: %+v", persisted[0])
	}
	if persisted[1].ThreadID != "cth-review" || persisted[1].Content != "private investigation" {
		t.Fatalf("subthread assistant row not preserved: %+v", persisted[1])
	}
	if persisted[2].ThreadID != "cth-review" || persisted[2].Role != "participant" || persisted[2].ParticipantID != "prt-reviewer" {
		t.Fatalf("subthread participant row not preserved: %+v", persisted[2])
	}
	if persisted[3].Role != "meta" || persisted[3].InputTokens != 1 {
		t.Fatalf("main meta row not preserved: %+v", persisted[3])
	}
}

// buildEnvelopeSeqResyncFixture renders two generated envelope tags the way
// residentEnvelopeUserMessage stores them (coalesceEnvelopes for the content,
// envelopeMetaJSON for the meta), so resync tests exercise the exact persisted
// shape rather than hand-written approximations.
func buildEnvelopeSeqResyncFixture(t *testing.T) (string, []MessageEnvelope) {
	t.Helper()
	envs := []MessageEnvelope{
		{ID: "env-a", SourceThreadID: "thread-1", SourceTitle: "Room", SenderKind: "user", SenderName: "User", SourceSeq: 3, Text: "first"},
		{ID: "env-b", SourceThreadID: "thread-1", SourceTitle: "Room", SenderKind: "participant", SenderName: "Bea", Addressed: true, Hop: 1, SourceSeq: 4, Text: "second"},
	}
	return coalesceEnvelopes(envs), envs
}

func TestSyncIncomingMessageSourceSeqsRemapsByEnvelopeID(t *testing.T) {
	content, envs := buildEnvelopeSeqResyncFixture(t)
	remapped := append([]MessageEnvelope(nil), envs...)
	remapped[0].SourceSeq = 7
	remapped[1].SourceSeq = 9

	synced, unmatched := resyncIncomingMessageSourceSeqs(content, envelopeMetaJSON(remapped))
	if len(unmatched) != 0 {
		t.Fatalf("unexpected unmatched meta entries: %v", unmatched)
	}
	if !strings.Contains(synced, `envelope_id="env-a" seq="7"`) {
		t.Fatalf("first tag not remapped to seq 7: %q", synced)
	}
	if !strings.Contains(synced, `envelope_id="env-b" seq="9"`) {
		t.Fatalf("second tag not remapped to seq 9: %q", synced)
	}
	if strings.Contains(synced, `seq="3"`) || strings.Contains(synced, `seq="4"`) {
		t.Fatalf("stale seq attribute survived remap: %q", synced)
	}
	if !strings.Contains(synced, "\nfirst\n") || !strings.Contains(synced, "\nsecond\n") {
		t.Fatalf("envelope bodies disturbed by remap: %q", synced)
	}

	// A no-op resync (meta already in step with the text) must return the
	// content byte-for-byte unchanged.
	same, unmatched := resyncIncomingMessageSourceSeqs(content, envelopeMetaJSON(envs))
	if len(unmatched) != 0 || same != content {
		t.Fatalf("in-step resync must be a no-op, got unmatched=%v content=%q", unmatched, same)
	}
}

// TestSyncIncomingMessageSourceSeqsLeavesPastedLiteralTags is the corruption
// regression: a user message whose BODY quotes the envelope format verbatim
// must never have seq attributes injected into or stripped from that quoted
// text, even when the literal tag count happens to equal the meta count (the
// failure mode of the former count-based positional rewrite).
func TestSyncIncomingMessageSourceSeqsLeavesPastedLiteralTags(t *testing.T) {
	envs := []MessageEnvelope{{
		ID: "env-a", SourceThreadID: "thread-1", SourceTitle: "Room",
		SenderKind: "user", SenderName: "User", SourceSeq: 3,
		Text: "look at this transcript:\n<incoming_message thread=\"Old\" thread_id=\"thread-9\" from=\"user\" sender=\"User\" addressed=\"false\" hop=\"0\" seq=\"42\">\nquoted\n</incoming_message>",
	}}
	content := coalesceEnvelopes(envs)
	envs[0].SourceSeq = 7

	synced, unmatched := resyncIncomingMessageSourceSeqs(content, envelopeMetaJSON(envs))
	if len(unmatched) != 0 {
		t.Fatalf("unexpected unmatched meta entries: %v", unmatched)
	}
	if !strings.Contains(synced, `envelope_id="env-a" seq="7"`) {
		t.Fatalf("generated tag not remapped: %q", synced)
	}
	if !strings.Contains(synced, `seq="42"`) {
		t.Fatalf("pasted literal tag was rewritten: %q", synced)
	}

	// One pasted literal tag and one meta entry: the counts coincide, which
	// the old heuristic treated as a match. The literal tag has no
	// envelope_id, so the text must come back untouched and the meta entry
	// must surface as unmatched.
	pasted := "here is the prompt format I saw:\n<incoming_message thread=\"Room\" thread_id=\"thread-1\" from=\"user\" sender=\"User\" addressed=\"false\" hop=\"0\" seq=\"42\">\nquoted\n</incoming_message>"
	synced, unmatched = resyncIncomingMessageSourceSeqs(pasted, envelopeMetaJSON(envs))
	if synced != pasted {
		t.Fatalf("user-authored literal tag modified: %q", synced)
	}
	if len(unmatched) != 1 || !strings.Contains(unmatched[0], `envelope_id="env-a"`) || !strings.Contains(unmatched[0], `source_thread="thread-1"`) {
		t.Fatalf("unmatched meta entry not surfaced with identifiers: %v", unmatched)
	}
}

// TestSyncIncomingMessageSourceSeqsSurfacesMismatches covers the desync half:
// meta entries that cannot be anchored to exactly one envelope_id-stamped tag
// are reported, never silently dropped, and the text is left as written.
func TestSyncIncomingMessageSourceSeqsSurfacesMismatches(t *testing.T) {
	content, envs := buildEnvelopeSeqResyncFixture(t)

	// Rows persisted before tags carried envelope_id: same metas, but the
	// stored text has no envelope_id attributes. Nothing is rewritten and
	// every meta entry surfaces.
	legacy := strings.ReplaceAll(strings.ReplaceAll(content, ` envelope_id="env-a"`, ""), ` envelope_id="env-b"`, "")
	synced, unmatched := resyncIncomingMessageSourceSeqs(legacy, envelopeMetaJSON(envs))
	if synced != legacy {
		t.Fatalf("legacy content without envelope_id must stay as written: %q", synced)
	}
	if len(unmatched) != 2 {
		t.Fatalf("expected both legacy meta entries surfaced, got %v", unmatched)
	}

	// A meta entry whose id names no tag in the content (genuine
	// desynchronization) surfaces with identifiers while the other entry is
	// still remapped.
	orphan := append([]MessageEnvelope(nil), envs...)
	orphan[1].ID = "env-missing"
	orphan[0].SourceSeq = 7
	synced, unmatched = resyncIncomingMessageSourceSeqs(content, envelopeMetaJSON(orphan))
	if !strings.Contains(synced, `envelope_id="env-a" seq="7"`) {
		t.Fatalf("matched entry not remapped alongside mismatch: %q", synced)
	}
	if len(unmatched) != 1 || !strings.Contains(unmatched[0], `envelope_id="env-missing"`) {
		t.Fatalf("orphaned meta entry not surfaced: %v", unmatched)
	}

	// A meta entry without an id has no deterministic key at all: surfaced,
	// never guessed into a tag.
	keyless := []MessageEnvelope{{SourceThreadID: "thread-1", SourceSeq: 5, Text: "x"}}
	synced, unmatched = resyncIncomingMessageSourceSeqs(content, envelopeMetaJSON(keyless))
	if synced != content {
		t.Fatalf("keyless meta entry must not rewrite anything: %q", synced)
	}
	if len(unmatched) != 1 {
		t.Fatalf("keyless meta entry not surfaced: %v", unmatched)
	}

	// An envelope_id duplicated in the content (the model or user echoed a
	// generated tag verbatim) is ambiguous: no rewrite, surfaced.
	duplicated := content + "\n\nechoed:\n" + content
	synced, unmatched = resyncIncomingMessageSourceSeqs(duplicated, envelopeMetaJSON(envs[:1]))
	if synced != duplicated {
		t.Fatalf("ambiguous duplicate envelope_id must not rewrite anything: %q", synced)
	}
	if len(unmatched) != 1 || !strings.Contains(unmatched[0], `envelope_id="env-a"`) {
		t.Fatalf("ambiguous meta entry not surfaced: %v", unmatched)
	}
}
