package agent

import (
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestAssembleModelRequestRequiresExplicitRequestOnlyPolicy(t *testing.T) {
	history := []providers.ChatMessage{{Role: "user", Content: "hi"}}
	implicit := ContextSegment{
		Messages: []providers.ChatMessage{{Role: "user", Content: "implicit context"}},
	}

	assembly := assembleModelRequest(history, []ContextSegment{implicit})
	if len(assembly.Messages) != 1 || len(assembly.RequestOnlyMessages) != 0 || len(assembly.Segments) != 0 {
		t.Fatalf("zero-value segment policy should not enter provider request: %+v", assembly)
	}

	explicit := RequestOnlyContextSegment([]providers.ChatMessage{{Role: "user", Content: "explicit context"}})
	assembly = assembleModelRequest(history, []ContextSegment{explicit})
	if len(assembly.Messages) != 2 || len(assembly.RequestOnlyMessages) != 1 || len(assembly.Segments) != 1 {
		t.Fatalf("explicit request-only segment should enter provider request: %+v", assembly)
	}
	if !assembly.Messages[1].Hidden || assembly.Messages[1].Content != "explicit context" {
		t.Fatalf("explicit request-only projection not normalized: %+v", assembly.Messages[1])
	}
}
