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
