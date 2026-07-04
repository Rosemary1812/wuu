package appserver

import (
	"context"
	"fmt"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
)

// The wire ThreadItem for a chat message carries its stable seq, so the chat
// view can map read receipts and reactions (both keyed by seq) to the bubble.
// Verified on the live path, and that the value matches the stored row (which
// the reload path reads back through the same chatMessageItem builder).
func TestThreadItemCarriesMessageSeq(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	groupID := startGroupThreadForTest(t, srv)

	raw := fmt.Sprintf(`{"id":"turn","method":"turn/start","params":{"thread_id":%q,"prompt":"hello"}}`, groupID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	th := srv.thread(groupID)
	if th == nil {
		t.Fatal("thread missing")
	}
	th.mu.Lock()
	turns := th.Turns
	th.mu.Unlock()
	itemSeq := 0
	for _, turn := range turns {
		for _, it := range turn.Items {
			if it.Type == ThreadItemUserMessage {
				itemSeq = it.Seq
			}
		}
	}
	if itemSeq <= 0 {
		t.Fatalf("user_message item Seq = %d, want > 0", itemSeq)
	}

	// The item's seq must equal the persisted row's seq — the address the
	// reload path (and thread/marks) uses.
	records, err := session.LoadHistoryRecords(rt.SessionDir, groupID, false)
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	storedSeq := 0
	for _, r := range records {
		if r.Role == "user" {
			storedSeq = r.Seq
		}
	}
	if storedSeq != itemSeq {
		t.Fatalf("wire item seq %d != stored row seq %d", itemSeq, storedSeq)
	}
}
