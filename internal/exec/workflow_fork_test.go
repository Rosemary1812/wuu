package exec

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/hooks"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// scriptedWorkerClient is a deterministic provider for a named-agent worker
// turn: it replays a fixed queue of chat responses (each optionally carrying
// tool calls) so a participant/start run emits an exact tool sequence — here a
// manage_participant{action:fork} call followed by a final message — with no
// live API.
type scriptedWorkerClient struct {
	mu        sync.Mutex
	responses []providers.ChatResponse
}

func (c *scriptedWorkerClient) Chat(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.responses) == 0 {
		return providers.ChatResponse{Content: "done"}, nil
	}
	res := c.responses[0]
	c.responses = c.responses[1:]
	return res, nil
}

// newExecForkTestRuntime mirrors newExecGroupTestRuntime but wires a worker
// client and a wuu home / state dir so participant/start can spawn a real
// named-agent worker (with the resident tool surface) whose tool calls come
// from the deterministic scriptedWorkerClient.
func newExecForkTestRuntime(t *testing.T, worker providers.StreamClient) *runtime.Session {
	t.Helper()
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	wuuHome := filepath.Join(home, ".wuu")
	stateDir := filepath.Join(wuuHome, "workspaces", "exec-fork")
	kit, err := tools.New(root)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	return &runtime.Session{
		ProviderName:   "fake-provider",
		Model:          "fake-model",
		RootDir:        root,
		WorkspaceID:    "exec-fork",
		StateDir:       stateDir,
		WuuHome:        wuuHome,
		ConfigPath:     root + "/.wuu.json",
		SessionDir:     statepath.SessionsDir(wuuHome),
		HookDispatcher: hooks.NewDispatcher(nil),
		Toolkit:        kit,
		WorkerClient:   worker,
		StreamRunner: &agent.StreamRunner{
			Client:       providers.AdaptStreamClient(execGroupFakeClient{}),
			Model:        "fake-model",
			SystemPrompt: "system prompt",
			MaxSteps:     4,
		},
	}
}

// seedMotherParticipant registers an active named agent with an on-disk memory
// notebook holding one durable topic, so a fork of it starts from a real memory
// snapshot (and a later merge-back has a mother notebook to merge into).
func seedMotherParticipant(t *testing.T, rt *runtime.Session, name string) participant.Participant {
	t.Helper()
	id := participant.NewID()
	workspace := filepath.Join(rt.WuuHome, "participants", id)
	mother := participant.Participant{
		ID:        id,
		Kind:      participant.KindNamed,
		Name:      name,
		Role:      "reviewer",
		Workspace: workspace,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := session.UpsertParticipant(rt.SessionDir, mother); err != nil {
		t.Fatalf("seed mother: %v", err)
	}
	writeExecNotebookTopic(t, workspace, "shared.md", "Shared", "A shared unchanged lesson.")
	return mother
}

func writeExecNotebookTopic(t *testing.T, workspace, file, name, body string) {
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

// End-to-end through exec against the REAL app server (over pipes, deterministic
// worker): exec drives a named-participant turn whose scripted provider emits a
// manage_participant{action:fork} tool call, then retires the resulting fork over
// RPC. Asserts the exec-driven fork lifecycle: the fork is created as a memory-
// backed copy (ForkedFrom the mother), and retiring it append-merges its new
// notebook topic back into the mother before archival.
func TestExecDrivesForkAndRetireMergesMemoryBack(t *testing.T) {
	worker := &scriptedWorkerClient{responses: []providers.ChatResponse{
		{ToolCalls: []providers.ToolCall{{
			ID:        "call_fork",
			Name:      "manage_participant",
			Arguments: `{"action":"fork","name":"ada"}`,
		}}},
		{Content: "Forked a copy to help."},
	}}
	rt := newExecForkTestRuntime(t, providers.AdaptStreamClient(worker))
	mother := seedMotherParticipant(t, rt, "ada")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	controller := newLocalControllerForRuntime(ctx, rt)

	var stdout bytes.Buffer
	err := Run(ctx, Options{
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
		Actions: []GroupAction{
			{Action: "create_group", Params: map[string]any{"title": "Ship it"}, SaveAs: map[string]string{"group": "thread.id"}},
			{
				Action: "participant_turn",
				As:     mother.ID,
				Params: map[string]any{"thread_id": "$group"},
				Prompt: "You are short-handed. Fork a copy of yourself.",
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, stdout.String())
	}

	// The named turn's fork tool call created a memory-backed copy of the mother.
	fork := waitForForkOf(t, rt.SessionDir, mother.ID)
	if fork.Kind != participant.KindNamed {
		t.Fatalf("fork kind = %s, want named", fork.Kind)
	}
	if !strings.Contains(fork.Name, "ada") {
		t.Fatalf("fork should be named after its mother, got %q", fork.Name)
	}
	// The mother's notebook snapshot copied into the fork.
	shared := filepath.Join(fork.Workspace, "memory", "shared.md")
	if _, err := os.Stat(shared); err != nil {
		t.Fatalf("fork should start with the mother's notebook snapshot: %v", err)
	}

	// The fork learns something new while running.
	writeExecNotebookTopic(t, fork.Workspace, "new-lesson.md", "New lesson", "The flake is a race in the poller.")

	// Exec retires the fork over RPC (a fresh controller: Run shuts the first one
	// down on return); retirement append-merges its new topic back into the
	// mother before archival.
	retireController := newLocalControllerForRuntime(ctx, rt)
	if err := Run(ctx, Options{
		JSON:       true,
		Stdout:     &bytes.Buffer{},
		Controller: retireController,
		Actions: []GroupAction{
			{Action: "retire_participant", Params: map[string]any{"participant_id": fork.ID}},
		},
	}); err != nil {
		t.Fatalf("retire Run: %v", err)
	}

	motherNotebook := filepath.Join(mother.Workspace, "memory")
	if _, err := os.Stat(filepath.Join(motherNotebook, "new-lesson.md")); err != nil {
		t.Fatalf("mother should gain the fork's new topic after merge-back: %v", err)
	}

	// The fork left the active roster (retired, not deleted).
	retired, err := session.GetParticipant(rt.SessionDir, fork.ID)
	if err != nil {
		t.Fatalf("retired fork should still be readable: %v", err)
	}
	if retired.RetiredAt == nil {
		t.Fatalf("fork should carry RetiredAt after retire")
	}
}

// waitForForkOf polls the roster for a fork of motherID: the participant/start
// run is asynchronous, and although exec waits for the agent to reach a terminal
// status, the fork row write races that terminal notification slightly on some
// runs. Poll briefly to keep the assertion deterministic.
func waitForForkOf(t *testing.T, sessDir, motherID string) participant.Participant {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		all, err := session.ListParticipants(sessDir, participant.KindNamed)
		if err != nil {
			t.Fatalf("list participants: %v", err)
		}
		for _, p := range all {
			if strings.TrimSpace(p.ForkedFrom) == motherID {
				return p
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no fork of %q appeared in the roster: %+v", motherID, all)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
