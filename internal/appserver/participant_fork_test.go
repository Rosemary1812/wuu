package appserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/session"
)

// writeNotebookTopic writes a memdir topic file (with frontmatter) into a
// participant's identity notebook (workspace/memory), simulating a durable note
// the resident accumulated while participating.
func writeNotebookTopic(t *testing.T, workspace, file, name, body string) {
	t.Helper()
	dir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir notebook: %v", err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\ntype: lesson\n---\n\n%s\n", name, name, body)
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatalf("write topic %s: %v", file, err)
	}
}

// TestManageParticipantForkCreatesMemoryBackedCopy proves action=fork makes a
// KindNamed分身 that (a) is marked ForkedFrom the母体, (b) is named "<母体> 的分身",
// and (c) starts with a COPY of the母体's memory notebook — not an empty shell.
func TestManageParticipantForkCreatesMemoryBackedCopy(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	srv := New(rt, &lockedBuffer{})

	if _, err := executeManageParticipant(t, srv, "agent-1", `{"action":"save","name":"Rina","role":"reviewer","memory_seed":"seed"}`); err != nil {
		t.Fatalf("save mother: %v", err)
	}
	mother := activeNamedByName(t, rt.SessionDir, "Rina")
	// Rina accumulated a durable note in her identity notebook.
	writeNotebookTopic(t, mother.Workspace, "deploy.md", "Deploy flow", "Run the deploy script twice.")

	out, err := executeManageParticipant(t, srv, "agent-1", `{"action":"fork","name":"Rina"}`)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	if !strings.Contains(out, `"forked_from_id":`) || !strings.Contains(out, mother.ID) {
		t.Fatalf("fork result should carry forked_from_id=%s, got %s", mother.ID, out)
	}
	if !strings.Contains(out, "Rina 的分身") {
		t.Fatalf("fork should be named 'Rina 的分身', got %s", out)
	}

	fork := activeNamedByName(t, rt.SessionDir, "Rina 的分身")
	if fork.Kind != participant.KindNamed {
		t.Fatalf("fork kind = %s, want named", fork.Kind)
	}
	if fork.ForkedFrom != mother.ID {
		t.Fatalf("fork ForkedFrom = %q, want %q", fork.ForkedFrom, mother.ID)
	}
	if fork.Role != "reviewer" {
		t.Fatalf("fork should inherit母体 role, got %q", fork.Role)
	}
	// The母体's notebook topic was snapshot-copied into the fork.
	copied := filepath.Join(fork.Workspace, "memory", "deploy.md")
	if data, err := os.ReadFile(copied); err != nil {
		t.Fatalf("fork should start with母体's notebook memory: %v", err)
	} else if !strings.Contains(string(data), "Run the deploy script twice.") {
		t.Fatalf("copied notebook topic should hold母体's note, got:\n%s", data)
	}
}

// TestForkRetireMergesMemoryBackAndArchives proves the full回流 lifecycle:
// retiring a分身 append-merges its NEW notebook topics into the母体, does not
// duplicate the shared ones, and archives the fork (removes it from the active
// roster, stamps RetiredAt) without deleting anything.
func TestForkRetireMergesMemoryBackAndArchives(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	srv := New(rt, &lockedBuffer{})

	if _, err := executeManageParticipant(t, srv, "agent-1", `{"action":"save","name":"Kenta","role":"qa","memory_seed":"seed"}`); err != nil {
		t.Fatalf("save mother: %v", err)
	}
	mother := activeNamedByName(t, rt.SessionDir, "Kenta")
	// A shared topic both notebooks will hold identically (must be de-duped).
	writeNotebookTopic(t, mother.Workspace, "shared.md", "Shared", "A shared unchanged lesson.")

	forkP, err := srv.forkNamedParticipant(mother)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	// The fork learns something new while running.
	writeNotebookTopic(t, forkP.Workspace, "new-lesson.md", "New lesson", "The flake is a race in the poller.")

	// Retiring the fork triggers merge-back then archival.
	if err := srv.retireNamedParticipant(forkP); err != nil {
		t.Fatalf("retire fork: %v", err)
	}

	motherNotebook := filepath.Join(mother.Workspace, "memory")
	if _, err := os.Stat(filepath.Join(motherNotebook, "new-lesson.md")); err != nil {
		t.Fatalf("母体 should gain the fork's new topic after merge-back: %v", err)
	}
	// The shared topic must not be duplicated under a conflict name.
	if _, err := os.Stat(filepath.Join(motherNotebook, "shared-fork.md")); !os.IsNotExist(err) {
		t.Fatalf("shared topic must not be duplicated, stat err = %v", err)
	}
	idx, err := os.ReadFile(filepath.Join(motherNotebook, "MEMORY.md"))
	if err == nil && strings.Count(string(idx), "new-lesson.md") != 1 {
		t.Fatalf("index should reference the merged topic exactly once, got:\n%s", idx)
	}

	// The fork left the active roster and was retired (not deleted).
	if _, err := activeNamedByNameOrNil(rt.SessionDir, "Kenta 的分身"); err == nil {
		t.Fatalf("fork should no longer be an active named agent")
	}
	retired, err := session.GetParticipant(rt.SessionDir, forkP.ID)
	if err != nil {
		t.Fatalf("retired fork should still be readable: %v", err)
	}
	if retired.RetiredAt == nil {
		t.Fatalf("fork should carry RetiredAt after retire")
	}
	// The母体 stays active and unforked.
	stillMother := activeNamedByName(t, rt.SessionDir, "Kenta")
	if stillMother.ForkedFrom != "" {
		t.Fatalf("母体 must not be marked as a fork")
	}
}

// TestMergeForkBackSkippedWhenMotherRetired proves merge-back is best-effort:
// a retired母体 is skipped (no panic, no error) and the fork still retires.
func TestMergeForkBackSkippedWhenMotherRetired(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	srv := New(rt, &lockedBuffer{})

	if _, err := executeManageParticipant(t, srv, "agent-1", `{"action":"save","name":"Noa","role":"planner","memory_seed":"seed"}`); err != nil {
		t.Fatalf("save mother: %v", err)
	}
	mother := activeNamedByName(t, rt.SessionDir, "Noa")
	forkP, err := srv.forkNamedParticipant(mother)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	// Retire the母体 first.
	if err := srv.retireNamedParticipant(mother); err != nil {
		t.Fatalf("retire mother: %v", err)
	}
	// Retiring the fork must not fail even though the母体 is gone.
	if err := srv.retireNamedParticipant(forkP); err != nil {
		t.Fatalf("retire fork with retired mother: %v", err)
	}
}

func activeNamedByNameOrNil(sessionDir, name string) (participant.Participant, error) {
	all, err := session.ListParticipants(sessionDir, participant.KindNamed)
	if err != nil {
		return participant.Participant{}, err
	}
	for _, p := range all {
		if strings.EqualFold(p.Name, name) {
			return p, nil
		}
	}
	return participant.Participant{}, fmt.Errorf("participant %q not active", name)
}

func activeNamedByName(t *testing.T, sessionDir, name string) participant.Participant {
	t.Helper()
	p, err := activeNamedByNameOrNil(sessionDir, name)
	if err != nil {
		t.Fatalf("active named %q: %v", name, err)
	}
	return p
}
