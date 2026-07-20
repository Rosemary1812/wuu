package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func planReminderBlock(t *testing.T, kit *Toolkit) (wuucontext.Block, bool) {
	t.Helper()
	for _, block := range kit.ContextBlocks() {
		if block.Kind == wuucontext.BlockPlanReminder {
			return block, true
		}
	}
	return wuucontext.Block{}, false
}

func TestToolkit_PlanStaleReminder(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	if _, ok := planReminderBlock(t, kit); ok {
		t.Fatal("reminder must not appear without a plan")
	}

	if _, err := kit.Execute(ctx, providers.ToolCall{
		Name:      "update_plan",
		Arguments: `{"plan":[{"step":"inspect","status":"completed"},{"step":"edit","status":"in_progress"}]}`,
	}); err != nil {
		t.Fatalf("update_plan: %v", err)
	}

	readOnce := func(i int) {
		t.Helper()
		if _, err := kit.Execute(ctx, providers.ToolCall{
			Name:      "read_file",
			Arguments: fmt.Sprintf(`{"path":"f.txt","offset":%d}`, i),
		}); err != nil {
			t.Fatalf("read_file %d: %v", i, err)
		}
	}

	for i := 1; i < planStaleReminderCallThreshold; i++ {
		readOnce(i)
	}
	if _, ok := planReminderBlock(t, kit); ok {
		t.Fatalf("reminder must not appear before %d calls", planStaleReminderCallThreshold)
	}

	readOnce(planStaleReminderCallThreshold)
	block, ok := planReminderBlock(t, kit)
	if !ok {
		t.Fatal("reminder missing after threshold")
	}
	if !strings.Contains(block.Content, "update_plan") || !strings.Contains(block.Content, "- [in_progress] edit") {
		t.Fatalf("reminder content should nudge and restate the plan: %q", block.Content)
	}
	if wuucontext.IsDerivedLedgerBlockName(wuucontext.SystemReminderBlockMessageName(block, 0)) {
		t.Fatal("plan reminder must not be classified as a derived ledger")
	}

	// A fresh update hides the reminder again.
	if _, err := kit.Execute(ctx, providers.ToolCall{
		Name:      "update_plan",
		Arguments: `{"plan":[{"step":"inspect","status":"completed"},{"step":"edit","status":"in_progress"}]}`,
	}); err != nil {
		t.Fatalf("update_plan refresh: %v", err)
	}
	if _, ok := planReminderBlock(t, kit); ok {
		t.Fatal("reminder must clear after update_plan")
	}

	// Fully completed plans never remind, even past the threshold.
	if _, err := kit.Execute(ctx, providers.ToolCall{
		Name:      "update_plan",
		Arguments: `{"plan":[{"step":"inspect","status":"completed"},{"step":"edit","status":"completed"}]}`,
	}); err != nil {
		t.Fatalf("update_plan complete: %v", err)
	}
	for i := 0; i < planStaleReminderCallThreshold; i++ {
		readOnce(i)
	}
	if _, ok := planReminderBlock(t, kit); ok {
		t.Fatal("reminder must not appear for a completed plan")
	}
}
