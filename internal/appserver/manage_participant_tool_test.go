package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// executeManageParticipant wires an AgentControl into a toolkit that already
// has participant speech enabled for the given agent, then dispatches the
// manage_participant tool call. The AgentControl is configured with the
// server's roster callback so the test exercises the full tool path.
func executeManageParticipant(t *testing.T, srv *Server, agentID, args string) (string, error) {
	t.Helper()
	rt := srv.rt
	kit, err := tools.New(rt.RootDir)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	kit.SetSessionDir(rt.SessionDir)
	c, err := agentcontrol.New(agentcontrol.Config{
		Client:        providers.AdaptStreamClient(&fakeClient{}),
		DefaultModel:  "fake-model",
		ParentRepo:    rt.RootDir,
		WorktreeRoot:  filepath.Join(rt.RootDir, ".wuu", "worktrees"),
		SessionID:     "sess-" + agentID,
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return nil, nil },
	})
	if err != nil {
		t.Fatalf("agentcontrol.New: %v", err)
	}
	t.Cleanup(c.Close)
	c.SetParticipantRoster(srv.participantRosterForTool())
	c.EnableParticipantSpeech(agentID)
	kit.SetAgentControl(c)
	kit.SetAgentIdentity(agentID, string(agentthread.RootPath)+"/"+agentID)
	kit.SetParticipantSpeechEnabled(true)
	return kit.Execute(context.Background(), providers.ToolCall{Name: "manage_participant", Arguments: args})
}

func TestManageParticipantToolSaveCreatesNamedParticipant(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	srv := New(rt, &lockedBuffer{})

	out, err := executeManageParticipant(t, srv, "agent-1", `{"action":"save","name":"Mira","role":"reviewer","tagline":"review diffs","memory_seed":"remembers to be terse"}`)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !strings.Contains(out, `"name":"Mira"`) {
		t.Fatalf("expected save result to include name=Mira, got %s", out)
	}

	all, err := session.ListParticipants(rt.SessionDir, participant.KindNamed)
	if err != nil {
		t.Fatalf("list named: %v", err)
	}
	var found *participant.Participant
	for i, p := range all {
		if strings.EqualFold(p.Name, "Mira") {
			found = &all[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("Mira not found in active named participants: %+v", all)
	}
	if found.Role != "reviewer" {
		t.Errorf("role = %q, want reviewer", found.Role)
	}
	if found.Tagline != "review diffs" {
		t.Errorf("tagline = %q, want review diffs", found.Tagline)
	}
	if found.Workspace == "" {
		t.Errorf("workspace should be set for a new named participant")
	}
	memPath := filepath.Join(found.Workspace, participantMemoryFileName)
	data, err := os.ReadFile(memPath)
	if err != nil {
		t.Fatalf("MEMORY.md should exist for new participant: %v", err)
	}
	if !strings.Contains(string(data), "remembers to be terse") {
		t.Errorf("MEMORY.md should contain memory_seed, got %q", string(data))
	}
}

func TestManageParticipantToolSaveUpdateDoesNotOverwriteMemory(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	srv := New(rt, &lockedBuffer{})

	if _, err := executeManageParticipant(t, srv, "agent-1", `{"action":"save","name":"Noel","role":"reviewer","memory_seed":"original memory"}`); err != nil {
		t.Fatalf("first save: %v", err)
	}
	all, err := session.ListParticipants(rt.SessionDir, participant.KindNamed)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var noel *participant.Participant
	for i, p := range all {
		if strings.EqualFold(p.Name, "Noel") {
			noel = &all[i]
			break
		}
	}
	if noel == nil {
		t.Fatalf("Noel not found: %+v", all)
	}
	memPath := filepath.Join(noel.Workspace, participantMemoryFileName)
	if data, err := os.ReadFile(memPath); err != nil {
		t.Fatalf("read memory: %v", err)
	} else if !strings.Contains(string(data), "original memory") {
		t.Fatalf("initial memory missing: %q", string(data))
	}

	if _, err := executeManageParticipant(t, srv, "agent-1", `{"action":"save","name":"Noel","role":"qa","memory_seed":"THIS MUST NOT WIN"}`); err != nil {
		t.Fatalf("update save: %v", err)
	}
	data, err := os.ReadFile(memPath)
	if err != nil {
		t.Fatalf("read memory after update: %v", err)
	}
	if !strings.Contains(string(data), "original memory") {
		t.Errorf("update save must not touch MEMORY.md, got %q", string(data))
	}
	if strings.Contains(string(data), "THIS MUST NOT WIN") {
		t.Errorf("update save leaked memory_seed into existing memory: %q", string(data))
	}
	updated, err := session.GetParticipant(rt.SessionDir, noel.ID)
	if err != nil {
		t.Fatalf("get updated: %v", err)
	}
	if updated.Role != "qa" {
		t.Errorf("role = %q, want qa", updated.Role)
	}
}

func TestManageParticipantToolListOmitsMemory(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	srv := New(rt, &lockedBuffer{})

	if _, err := executeManageParticipant(t, srv, "agent-1", `{"action":"save","name":"Quinn","role":"qa","tagline":"smoke-tests everything"}`); err != nil {
		t.Fatalf("save Quinn: %v", err)
	}
	out, err := executeManageParticipant(t, srv, "agent-1", `{"action":"list"}`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var payload struct {
		Participants []map[string]any `json:"participants"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal list payload %q: %v", out, err)
	}
	if len(payload.Participants) == 0 {
		t.Fatalf("list returned no participants: %s", out)
	}
	var quinn map[string]any
	for _, p := range payload.Participants {
		if p["name"] == "Quinn" {
			quinn = p
		}
		if _, ok := p["memory"]; ok {
			t.Errorf("list result must not include memory, got %+v", p)
		}
		if _, ok := p["name"]; !ok {
			t.Errorf("list entry missing name: %+v", p)
		}
		if _, ok := p["role"]; !ok {
			t.Errorf("list entry missing role: %+v", p)
		}
	}
	if quinn == nil {
		t.Fatalf("Quinn not in list: %+v", payload.Participants)
	}
	if quinn["tagline"] != "smoke-tests everything" {
		t.Errorf("Quinn tagline = %v, want smoke-tests everything", quinn["tagline"])
	}
}

func TestManageParticipantToolRetireRemovesFromActiveList(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	srv := New(rt, &lockedBuffer{})

	if _, err := executeManageParticipant(t, srv, "agent-1", `{"action":"save","name":"Riley","role":"qa"}`); err != nil {
		t.Fatalf("save Riley: %v", err)
	}
	all, err := session.ListParticipants(rt.SessionDir, participant.KindNamed)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var rileyID string
	for _, p := range all {
		if strings.EqualFold(p.Name, "Riley") {
			rileyID = p.ID
			break
		}
	}
	if rileyID == "" {
		t.Fatalf("Riley not found in list: %+v", all)
	}

	if _, err := executeManageParticipant(t, srv, "agent-1", fmt.Sprintf(`{"action":"retire","name":"Riley"}`)); err != nil {
		t.Fatalf("retire: %v", err)
	}
	all, err = session.ListParticipants(rt.SessionDir, participant.KindNamed)
	if err != nil {
		t.Fatalf("list after retire: %v", err)
	}
	for _, p := range all {
		if p.ID == rileyID {
			t.Errorf("retired participant %q should be removed from active list", p.Name)
		}
	}
}

func TestManageParticipantToolRetireSelfRejected(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	srv := New(rt, &lockedBuffer{})

	// Seed a participant named "Sloan" so the roster has a real target
	// that matches the agent's own name.
	if _, err := executeManageParticipant(t, srv, "agent-1", `{"action":"save","name":"Sloan","role":"qa"}`); err != nil {
		t.Fatalf("save Sloan: %v", err)
	}

	// Now wire AgentControl so the agent's own ParticipantID resolves to Sloan
	// and ask Sloan to retire herself.
	kit, err := tools.New(rt.RootDir)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	kit.SetSessionDir(rt.SessionDir)
	c, err := agentcontrol.New(agentcontrol.Config{
		Client:        providers.AdaptStreamClient(&fakeClient{}),
		DefaultModel:  "fake-model",
		ParentRepo:    rt.RootDir,
		WorktreeRoot:  filepath.Join(rt.RootDir, ".wuu", "worktrees"),
		SessionID:     "sess-self",
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return nil, nil },
	})
	if err != nil {
		t.Fatalf("agentcontrol.New: %v", err)
	}
	t.Cleanup(c.Close)
	c.SetParticipantRoster(srv.participantRosterForTool())
	c.EnableParticipantSpeech("agent-self")

	// Look up Sloan's participant record so we can pin the agent's
	// ParticipantID to it.
	all, err := session.ListParticipants(rt.SessionDir, participant.KindNamed)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var slparticipantID string
	for _, p := range all {
		if strings.EqualFold(p.Name, "Sloan") {
			slparticipantID = p.ID
			break
		}
	}
	if slparticipantID == "" {
		t.Fatalf("Sloan not found: %+v", all)
	}
	c.SetAgentParticipantID("agent-self", slparticipantID)
	srv.RegisterAgentParticipantName("agent-self", "Sloan")

	kit.SetAgentControl(c)
	kit.SetAgentIdentity("agent-self", string(agentthread.RootPath)+"/agent-self")
	kit.SetParticipantSpeechEnabled(true)
	_, err = kit.Execute(context.Background(), providers.ToolCall{Name: "manage_participant", Arguments: `{"action":"retire","name":"Sloan"}`})
	if err == nil {
		t.Fatalf("retire self should error, got nil")
	}
	if !strings.Contains(err.Error(), "cannot retire yourself") {
		t.Fatalf("retire self error = %q, want 'cannot retire yourself'", err.Error())
	}
	all, err = session.ListParticipants(rt.SessionDir, participant.KindNamed)
	if err != nil {
		t.Fatalf("list after failed self-retire: %v", err)
	}
	for _, p := range all {
		if p.ID == slparticipantID && p.RetiredAt != nil {
			t.Errorf("self-retire must not retire Sloan, got %+v", p)
		}
	}
}

func TestManageParticipantToolRejectsInvalidRole(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	srv := New(rt, &lockedBuffer{})

	_, err := executeManageParticipant(t, srv, "agent-1", `{"action":"save","name":"Theo","role":"not-a-real-role"}`)
	if err == nil {
		t.Fatalf("invalid role should error")
	}
	if !strings.Contains(err.Error(), "not-a-real-role") {
		t.Fatalf("error should mention the invalid role, got %v", err)
	}
}
