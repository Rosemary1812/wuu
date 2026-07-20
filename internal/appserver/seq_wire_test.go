package appserver

import (
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestThreadItemCarriesMessageSeq(t *testing.T) {
	item := chatMessageItem("item-user", providers.ChatMessage{
		Role:    "user",
		Content: "hello",
		Seq:     42,
	})
	if item.Type != ThreadItemUserMessage || item.Seq != 42 {
		t.Fatalf("user message item = %+v, want stable seq 42", item)
	}
}
