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
