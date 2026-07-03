package appserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// TestResidentToolkitEnforcesWorkspaceFileScope verifies the S7 wiring end
// to end: a resident runtime's file tools may write inside a registered
// workspace root (outside the agent home) and are rejected with the
// user-facing guidance everywhere else.
func TestResidentToolkitEnforcesWorkspaceFileScope(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	srv.rt.WuuHome = filepath.Join(srv.rt.RootDir, ".wuu")
	workspaceRoot := t.TempDir()
	store := fmt.Sprintf(`{"projects":[{"id":"p","name":"demo","path":%q}]}`, workspaceRoot)
	if err := os.MkdirAll(srv.rt.WuuHome, 0o755); err != nil {
		t.Fatalf("mkdir wuu home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srv.rt.WuuHome, "projects.json"), []byte(store), 0o644); err != nil {
		t.Fatalf("write projects.json: %v", err)
	}

	participantID := saveNamedParticipant(t, srv.rt, "Iris", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, participantID)
	kit := residentToolkitForTest(t, srv, dmID)

	inside := filepath.Join(workspaceRoot, "notes.md")
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: fmt.Sprintf(`{"path":%q,"content":"ok"}`, inside),
	}); err != nil {
		t.Fatalf("write inside registered workspace should succeed: %v", err)
	}

	// Every t.TempDir() lives under the system temp root, which the
	// production whitelist includes — so exercise a genuinely
	// out-of-scope path outside temp entirely. The scope check rejects
	// before any filesystem access, so the path need not exist.
	blocked := filepath.Join(string(filepath.Separator), "etc", "wuu-blocked", "file.txt")
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: fmt.Sprintf(`{"path":%q}`, blocked),
	}); err == nil || !strings.Contains(err.Error(), "该路径不在工作区内") {
		t.Fatalf("read outside the whitelist should fail with guidance, got err=%v", err)
	}
}
