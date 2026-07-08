package tools

import (
	"strings"
	"testing"
)

func TestManageTaskToolDescriptionSplitsVisiblePostFromPieceDone(t *testing.T) {
	def := NewManageTaskTool(&Env{}).Definition()
	properties, ok := def.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("manage_task schema missing properties: %+v", def.InputSchema)
	}
	action, ok := properties["action"].(map[string]any)
	if !ok {
		t.Fatalf("manage_task schema missing action property: %+v", properties)
	}
	description, _ := action["description"].(string)
	for _, want := range []string{
		"call post_message alone first and wait for status=\"posted\" before piece_done",
		"Never call post_message and piece_done in the same assistant tool-call batch",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("manage_task action description missing %q:\n%s", want, description)
		}
	}
}
