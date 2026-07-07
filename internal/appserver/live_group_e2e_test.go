//go:build wuu_live_e2e

// Live end-to-end group-chat harness. Boots a REAL model-backed app-server
// (the configured provider — MiniMax-M3 in dev) in an isolated sandbox home,
// creates a named-agent group, injects a user message through the EXACT human
// send path (turn/start -> handleGroupTurnStart -> routeUserMessageToResidents),
// lets the residents reply asynchronously, waits for the round to settle, then
// dumps what a human would actually see: the ordered final posted messages in
// the group (session.ChatMessagesSince), plus a per-agent mechanism trace
// (which tools each turn used — post_message held/sent, inception, pull).
//
// Gated by build tag `wuu_live_e2e` AND env `WUU_LIVE_E2E=1` so it never runs
// in CI. No step/time caps beyond a generous safety deadline (a runaway spam
// cascade is a result to observe, not to wait on forever).
//
//	go test -tags wuu_live_e2e ./internal/appserver/ -run TestLiveCounting -v -timeout 40m
//	go test -tags wuu_live_e2e ./internal/appserver/ -run TestLiveNoteAppDebate -v -timeout 40m
package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
)

func requireLive(t *testing.T) {
	t.Helper()
	if os.Getenv("WUU_LIVE_E2E") != "1" {
		t.Skip("set WUU_LIVE_E2E=1 (and -tags wuu_live_e2e) to run the live model group e2e")
	}
}

// realConfigPath finds the user's provider config to seed the sandbox with.
func realConfigPath(t *testing.T) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(os.Getenv("WUU_HOME"), "config.json"),
		filepath.Join(home, ".wuu", "config.json"),
		filepath.Join(home, ".config", "wuu", "config.json"),
	}
	for _, c := range candidates {
		if strings.TrimSpace(c) == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	t.Fatalf("no provider config found in %v", candidates)
	return ""
}

// newLiveRuntime boots a real-provider runtime in an isolated short-path
// sandbox home, seeded with the user's real provider config. Short path
// (/tmp/wuu-live-*) keeps absolute paths well under the length that has tripped
// up weaker models' tool-arg fidelity.
func newLiveRuntime(t *testing.T) *runtime.Session {
	t.Helper()
	sandbox, err := os.MkdirTemp("/tmp", "wuu-live-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sandbox) })

	cfgDst := filepath.Join(sandbox, ".wuu", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgDst), 0o755); err != nil {
		t.Fatalf("mkdir .wuu: %v", err)
	}
	raw, err := os.ReadFile(realConfigPath(t))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if err := os.WriteFile(cfgDst, raw, 0o600); err != nil {
		t.Fatalf("write sandbox config: %v", err)
	}

	root := filepath.Join(sandbox, "work")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir work: %v", err)
	}
	initAppserverGitRepo(t, root)

	cfg, cfgPath, err := config.LoadFrom(root, sandbox)
	if err != nil {
		t.Fatalf("load sandbox config: %v", err)
	}
	rt, err := runtime.NewSession(runtime.Options{
		RootDir:    root,
		HomeDir:    sandbox,
		ConfigPath: cfgPath,
		Config:     cfg,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() { _, _ = rt.Cleanup() })
	t.Logf("live sandbox: %s  sessionDir=%s  provider=%s model=%s", sandbox, rt.SessionDir, rt.ProviderName, rt.Model)
	return rt
}

// liveGroupUserTurn injects a plain user message into a group through the exact
// human send path: turn/start on a group thread dispatches to
// handleGroupTurnStart -> routeUserMessageToResidents (verified identical to
// what `wuu exec resume <groupID> "<msg>"` reaches).
func liveGroupUserTurn(t *testing.T, srv *Server, groupID, prompt string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"id":     "user-turn",
		"method": "turn/start",
		"params": map[string]any{"thread_id": groupID, "prompt": prompt},
	})
	if err := srv.handleLine(context.Background(), payload); err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "user-turn")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("turn/start error: %v", errMsg)
	}
}

// liveBusy reports whether any resident work is in flight: a drain being set up
// OR any thread running a model turn. The draining flag clears the instant a
// turn is handed to its goroutine, so the running check is what covers the
// seconds a real model spends thinking.
func liveBusy(srv *Server) bool {
	srv.residentDrainMu.Lock()
	draining := len(srv.drainingResidentAgent) > 0
	srv.residentDrainMu.Unlock()
	if draining {
		return true
	}
	srv.mu.Lock()
	ths := make([]*threadState, 0, len(srv.threads))
	for _, th := range srv.threads {
		ths = append(ths, th)
	}
	srv.mu.Unlock()
	for _, th := range ths {
		th.mu.Lock()
		r := th.running
		th.mu.Unlock()
		if r {
			return true
		}
	}
	return false
}

// waitForLiveQuiesce blocks until the resident cascade has been idle for
// idleSettle continuously, or maxWait elapses (a spam cascade that never
// settles is a finding — the transcript dump shows it). Progress is logged so a
// slow live run is visibly alive.
func waitForLiveQuiesce(t *testing.T, srv *Server, rt *runtime.Session, groupID string, maxWait, idleSettle time.Duration) {
	t.Helper()
	deadline := time.Now().Add(maxWait)
	idleSince := time.Time{}
	lastCount := -1
	lastLog := time.Now()
	for time.Now().Before(deadline) {
		if liveBusy(srv) {
			idleSince = time.Time{}
		} else if idleSince.IsZero() {
			idleSince = time.Now()
		} else if time.Since(idleSince) >= idleSettle {
			t.Logf("settled after idle %s", idleSettle)
			return
		}
		if n := liveMainStreamCount(rt, groupID); n != lastCount || time.Since(lastLog) > 20*time.Second {
			t.Logf("[%s] group posts=%d busy=%v", time.Now().Format("15:04:05"), n, liveBusy(srv))
			lastCount = n
			lastLog = time.Now()
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Logf("hit max wait %s without settling — dumping what happened", maxWait)
}

func liveMainStreamCount(rt *runtime.Session, groupID string) int {
	recs, err := session.ChatMessagesSince(rt.SessionDir, groupID, 0)
	if err != nil {
		return -1
	}
	n := 0
	for _, r := range recs {
		if strings.TrimSpace(r.ThreadID) == "" {
			n++
		}
	}
	return n
}

// dumpLiveTranscript prints the human-visible group transcript (final posted
// messages in seq order) followed by a compact per-agent mechanism trace.
func dumpLiveTranscript(t *testing.T, rt *runtime.Session, groupID string, names map[string]string) {
	t.Helper()
	recs, err := session.ChatMessagesSince(rt.SessionDir, groupID, 0)
	if err != nil {
		t.Fatalf("ChatMessagesSince: %v", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n================ HUMAN-VISIBLE GROUP TRANSCRIPT (%s) ================\n", groupID)
	for _, r := range recs {
		if strings.TrimSpace(r.ThreadID) != "" {
			continue // cth/reply-subthread, not the main stream
		}
		who := "User"
		if strings.EqualFold(strings.TrimSpace(r.Role), "participant") {
			who = firstNonEmpty(names[strings.TrimSpace(r.ParticipantID)], r.Name, r.ParticipantID)
		}
		text := strings.Join(strings.Fields(firstNonEmpty(r.DisplayContent, r.Content)), " ")
		kind := r.PostKind
		if kind == "" {
			kind = "user"
		}
		fmt.Fprintf(&b, "[%3d] %-8s (%s): %s\n", r.Seq, who, kind, text)
	}
	fmt.Fprintf(&b, "================ %d main-stream posts ================\n", liveMainStreamCount(rt, groupID))
	t.Log(b.String())

	// Write full transcript + mechanism trace to files for offline analysis.
	outDir := filepath.Join(os.TempDir(), "wuu-live-out")
	_ = os.MkdirAll(outDir, 0o755)
	transcriptPath := filepath.Join(outDir, t.Name()+".transcript.txt")
	_ = os.WriteFile(transcriptPath, []byte(b.String()), 0o644)
	t.Logf("transcript written: %s", transcriptPath)

	dumpLiveMechanismTrace(t, rt, names, outDir)
}

// dumpLiveMechanismTrace writes each agent's DM-thread turn history (assistant
// tool calls + tool results) so we can see how often it used post_message
// (held vs sent), inception, and fetch_thread_messages — the "keep context
// clean" mechanisms.
func dumpLiveMechanismTrace(t *testing.T, rt *runtime.Session, names map[string]string, outDir string) {
	t.Helper()
	sessions, err := session.List(rt.SessionDir, 0)
	if err != nil {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n================ PER-AGENT MECHANISM TRACE ================\n")
	for _, sess := range sessions {
		pid := strings.TrimSpace(sess.DMParticipantID)
		if pid == "" {
			continue
		}
		name := firstNonEmpty(names[pid], pid)
		recs, err := session.LoadHistoryRecords(rt.SessionDir, sess.ID, true)
		if err != nil {
			continue
		}
		var calls []string
		lastText := ""
		for _, r := range recs {
			switch strings.ToLower(strings.TrimSpace(r.Role)) {
			case "assistant":
				if t := strings.TrimSpace(r.Content); t != "" {
					lastText = strings.Join(strings.Fields(t), " ")
				}
				if len(r.ToolCalls) > 0 {
					var tcs []struct {
						Name string `json:"name"`
					}
					_ = json.Unmarshal(r.ToolCalls, &tcs)
					for _, tc := range tcs {
						calls = append(calls, tc.Name)
					}
				}
			case "tool":
				body := strings.Join(strings.Fields(r.Content), " ")
				low := strings.ToLower(body)
				if strings.Contains(low, "held") && len(body) < 400 {
					calls = append(calls, "→held:"+liveTruncate(body, 160))
				} else if strings.Contains(low, "error") || strings.Contains(low, "not a member") || strings.Contains(low, "refus") {
					calls = append(calls, "→err:"+liveTruncate(body, 160))
				}
			}
		}
		fmt.Fprintf(&b, "%-8s: tools=[%s]\n           last_text=%s\n", name, strings.Join(calls, ", "), liveTruncate(lastText, 240))
	}
	t.Log(b.String())
	_ = os.WriteFile(filepath.Join(outDir, t.Name()+".mechanism.txt"), []byte(b.String()), 0o644)
}

func liveTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ensureNamedParticipant returns the id of a non-retired named agent with the
// given name, reusing an existing one (e.g. the auto-seeded "Andy") or creating
// it. Robust against the first-launch default seed and against re-runs.
func ensureNamedParticipant(t *testing.T, rt *runtime.Session, name string) string {
	t.Helper()
	existing, err := session.ListParticipants(rt.SessionDir, participant.KindNamed)
	if err != nil {
		t.Fatalf("list participants: %v", err)
	}
	for _, p := range existing {
		if p.RetiredAt == nil && strings.EqualFold(strings.TrimSpace(p.Name), name) {
			return p.ID
		}
	}
	return saveNamedParticipant(t, rt, name, "群成员", "")
}

// liveGroup wires N named agents into a fresh group and returns the group id and
// an id->name map for readable transcripts.
func liveGroup(t *testing.T, srv *Server, rt *runtime.Session, title string, agentNames []string) (string, map[string]string) {
	t.Helper()
	names := make(map[string]string, len(agentNames))
	group := startNamedGroupThreadForTest(t, srv, title)
	for _, n := range agentNames {
		id := ensureNamedParticipant(t, rt, n)
		names[id] = n
		resp := addThreadMemberForTest(t, srv, "add-"+id, group.ID, id)
		if errMsg, ok := resp["error"]; ok {
			t.Fatalf("add member %s: %v", n, errMsg)
		}
	}
	return group.ID, names
}

// TestLiveSmoke is the cheapest live check: one agent, one message, confirm a
// real model reply lands in the group as a posted message. Validates config,
// client wiring, and the resident round-trip before the bigger scenarios.
func TestLiveSmoke(t *testing.T) {
	requireLive(t)
	rt := newLiveRuntime(t)
	srv := New(rt, &lockedBuffer{})
	groupID, names := liveGroup(t, srv, rt, "冒烟", []string{"Andy"})

	liveGroupUserTurn(t, srv, groupID, "用一句话打个招呼。")
	waitForLiveQuiesce(t, srv, rt, groupID, 5*time.Minute, 10*time.Second)
	dumpLiveTranscript(t, rt, groupID, names)

	if liveMainStreamCount(rt, groupID) <= 1 {
		t.Fatalf("no agent reply landed — real-model round-trip broken")
	}
}

func TestLiveCounting(t *testing.T) {
	requireLive(t)
	rt := newLiveRuntime(t)
	srv := New(rt, &lockedBuffer{})
	groupID, names := liveGroup(t, srv, rt, "报数群", []string{"Andy", "Bella", "Carl", "Diana", "Ethan"})

	liveGroupUserTurn(t, srv, groupID, "请从 1 报数到 20。")
	waitForLiveQuiesce(t, srv, rt, groupID, 30*time.Minute, 15*time.Second)
	dumpLiveTranscript(t, rt, groupID, names)

	// Soft check: the run produced posts (assertions on exact round-robin are
	// evaluated by reading the dumped transcript, not asserted here — the point
	// of the harness is observation).
	if liveMainStreamCount(rt, groupID) <= 1 {
		t.Fatalf("no agent posts — the round never propagated")
	}
}

func TestLiveNoteAppDebate(t *testing.T) {
	requireLive(t)
	rt := newLiveRuntime(t)
	srv := New(rt, &lockedBuffer{})
	groupID, names := liveGroup(t, srv, rt, "选型讨论", []string{"Andy", "Bella", "Carl"})

	liveGroupUserTurn(t, srv, groupID, "我打算下载一个笔记软件,你们觉得 Notion 和 Obsidian 谁更好一点?")
	waitForLiveQuiesce(t, srv, rt, groupID, 30*time.Minute, 15*time.Second)
	dumpLiveTranscript(t, rt, groupID, names)

	if liveMainStreamCount(rt, groupID) <= 1 {
		t.Fatalf("no agent posts — the discussion never propagated")
	}
}
