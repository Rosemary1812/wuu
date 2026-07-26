package appserver

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/agent"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/runtime"
)

func TestActiveDocumentRequestContext(t *testing.T) {
	if got := activeDocumentRequestContext(nil); got != nil {
		t.Fatalf("nil document context = %#v, want nil", got)
	}
	if got := activeDocumentRequestContext(&ActiveDocument{Path: "  "}); got != nil {
		t.Fatalf("blank document context = %#v, want nil", got)
	}

	segments := activeDocumentRequestContext(&ActiveDocument{Path: " docs/plan.md "})
	if len(segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(segments))
	}
	segment := segments[0]
	if segment.Lifecycle != agent.ContextSegmentRequestOnly || segment.Durable || segment.VisibleInUI {
		t.Fatalf("document context lifecycle = %+v, want hidden request-only", segment)
	}
	if len(segment.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(segment.Blocks))
	}
	block := segment.Blocks[0]
	if block.Kind != wuucontext.BlockActiveFiles || block.Source != "desktop.document_focus" {
		t.Fatalf("block identity = %+v", block)
	}
	if !strings.Contains(block.Content, `"docs/plan.md"`) {
		t.Fatalf("block content %q does not identify the active document", block.Content)
	}
}

func TestManagementSurfaceRequestContextIsHiddenAndRequestOnly(t *testing.T) {
	if got := managementSurfaceRequestContext("  "); got != nil {
		t.Fatalf("blank management context = %#v, want nil", got)
	}
	segments := managementSurfaceRequestContext(`{"surface":"automations_catalog"}`)
	if len(segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(segments))
	}
	segment := segments[0]
	if segment.Lifecycle != agent.ContextSegmentRequestOnly || segment.Durable || segment.VisibleInUI {
		t.Fatalf("management context lifecycle = %+v, want hidden request-only", segment)
	}
	block := segment.Blocks[0]
	if block.Kind != wuucontext.BlockAdditionalContext || block.Source != "desktop.management_surface" {
		t.Fatalf("block identity = %+v", block)
	}
	if !strings.Contains(block.Content, "automations_catalog") {
		t.Fatalf("block content = %q", block.Content)
	}
}

func TestSkillsManagementThreadContextNamesDedicatedWritableRoots(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	wuuHome := t.TempDir()
	srv := &Server{rt: &runtime.Session{RootDir: root, HomeDir: home, WuuHome: wuuHome}}
	segments := srv.managementThreadRequestContext(&threadState{ManagementSurface: "skills"})
	if len(segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(segments))
	}
	segment := segments[0]
	if segment.Lifecycle != agent.ContextSegmentRequestOnly || segment.Durable || segment.VisibleInUI {
		t.Fatalf("skills authority lifecycle = %+v, want hidden request-only", segment)
	}
	content := segment.Blocks[0].Content
	for _, want := range []string{
		filepath.Join(wuuHome, "skills"),
		filepath.Join(home, ".codex", "skills"),
		"same-name override",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("skills authority context %q does not contain %q", content, want)
		}
	}
}

func TestActiveDocumentContextForTurnUsesLatestSteerSnapshot(t *testing.T) {
	base := append(
		agent.RequestOnlyContextBlocks([]wuucontext.Block{{
			Kind: wuucontext.BlockTaskState, Source: "test.task_state", Content: "keep me",
		}}),
		activeDocumentRequestContext(&ActiveDocument{Path: "docs/original.md"})...,
	)
	overridden := activeDocumentContextForTurn(base, true, &ActiveDocument{Path: "docs/latest.md"})
	if len(overridden) != 2 || !strings.Contains(overridden[1].Blocks[0].Content, `"docs/latest.md"`) {
		t.Fatalf("overridden context = %#v, want latest steer document", overridden)
	}
	if overridden[0].Blocks[0].Source != "test.task_state" {
		t.Fatalf("overridden context dropped non-document segment: %#v", overridden)
	}
	if strings.Contains(overridden[1].Blocks[0].Content, "original.md") {
		t.Fatalf("overridden context retained original document: %#v", overridden)
	}
	if cleared := activeDocumentContextForTurn(base, true, nil); len(cleared) != 1 || cleared[0].Blocks[0].Source != "test.task_state" {
		t.Fatalf("cleared context = %#v, want only non-document context", cleared)
	}
	retained := activeDocumentContextForTurn(base, false, nil)
	if len(retained) != 2 || !strings.Contains(retained[1].Blocks[0].Content, `"docs/original.md"`) {
		t.Fatalf("retained context = %#v, want original turn context", retained)
	}
}

func TestReleaseTurnExecutionClearsSteerDocumentSnapshot(t *testing.T) {
	th := &threadState{
		currentTurn:           "turn-1",
		activeSteerDocument:   &ActiveDocument{Path: "docs/latest.md"},
		activeSteerContextSet: true,
	}

	th.releaseTurnExecutionLocked("turn-1")

	if th.activeSteerDocument != nil || th.activeSteerContextSet {
		t.Fatalf(
			"steer document state survived turn release: document=%#v set=%t",
			th.activeSteerDocument,
			th.activeSteerContextSet,
		)
	}
}

func TestRemovingSteerDocumentOverrideRestoresPreviousSnapshot(t *testing.T) {
	th := &threadState{steerDocumentOverrides: []activeDocumentOverride{
		{steerID: "steer-1", document: &ActiveDocument{Path: "docs/first.md"}},
		{steerID: "steer-2", document: &ActiveDocument{Path: "docs/latest.md"}},
	}}
	th.applyLatestSteerDocumentOverrideLocked()

	th.removeSteerDocumentOverrideLocked("steer-2")

	if !th.activeSteerContextSet || th.activeSteerDocument == nil || th.activeSteerDocument.Path != "docs/first.md" {
		t.Fatalf("active steer document = %#v, want previous snapshot", th.activeSteerDocument)
	}
	th.removeSteerDocumentOverrideLocked("steer-1")
	if th.activeSteerContextSet || th.activeSteerDocument != nil {
		t.Fatalf("active steer document survived final unsteer: %#v", th.activeSteerDocument)
	}
}

func TestQueuedDocumentSnapshotDoesNotPersistRequestContextBlock(t *testing.T) {
	snapshot := turnRuntimeSnapshot{ActiveDocument: &ActiveDocument{Path: "docs/queued.md"}}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "desktop.document_focus") || strings.Contains(string(encoded), "ACTIVE_FILES") {
		t.Fatalf("queued snapshot persisted request-only context block: %s", encoded)
	}
}
