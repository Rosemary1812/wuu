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

func TestLoadChatMessagesSkipsParticipantRows(t *testing.T) {
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
	if len(loaded) != 2 {
		t.Fatalf("participant row should not be loaded into model history: %+v", loaded)
	}
	if loaded[0].Role != "user" || loaded[1].Role != "assistant" {
		t.Fatalf("unexpected model history: %+v", loaded)
	}
}
