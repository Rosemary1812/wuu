package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestHelpMeDefinitionDoesNotExposeMode(t *testing.T) {
	def := NewHelpMeTool(&Env{}).Definition()
	props, _ := def.InputSchema["properties"].(map[string]any)
	if _, ok := props["mode"]; ok {
		t.Fatalf("helpme schema should not expose mode: %#v", def.InputSchema)
	}
	if _, ok := props["timeout_ms"]; ok {
		t.Fatalf("helpme schema should not expose long synchronous timeout: %#v", def.InputSchema)
	}
	if _, ok := props["wait_ms"]; ok {
		t.Fatalf("helpme schema should not ask the model to choose wait duration: %#v", def.InputSchema)
	}
	for _, want := range []string{"returns immediately", "await_agents", "inception", "bounded HelpMe recovery summary", "raw parent/helper transcripts"} {
		if !strings.Contains(def.Description, want) {
			t.Fatalf("helpme description missing %q:\n%s", want, def.Description)
		}
	}
	for _, unwanted := range []string{"waits for it to finish", "joint compact marker"} {
		if strings.Contains(def.Description, unwanted) {
			t.Fatalf("helpme description should not promise synchronous compact path %q:\n%s", unwanted, def.Description)
		}
	}
}

func TestDecodeHelpMeArgsAcceptsSingleStringLists(t *testing.T) {
	var args helpMeArgs
	if err := decodeArgs(`{
		"reason": "stuck",
		"failed_attempts": "CSS visibility changed but the rail still did not render",
		"constraints": "preserve existing sidebar behavior",
		"evidence": "screenshot shows the rail missing"
		}`, &args); err != nil {
		t.Fatalf("decode helpme args: %v", err)
	}
	if got := args.FailedAttempts; len(got) != 1 || got[0] != "CSS visibility changed but the rail still did not render" {
		t.Fatalf("failed_attempts = %#v", got)
	}
	if got := args.Constraints; len(got) != 1 || got[0] != "preserve existing sidebar behavior" {
		t.Fatalf("constraints = %#v", got)
	}
	if got := args.Evidence; len(got) != 1 || got[0] != "screenshot shows the rail missing" {
		t.Fatalf("evidence = %#v", got)
	}
}

func TestDecodeHelpMeArgsRejectsObjectListField(t *testing.T) {
	var args helpMeArgs
	err := decodeArgs(`{"failed_attempts":{"summary":"still wrong"}}`, &args)
	if err == nil {
		t.Fatal("expected invalid failed_attempts type")
	}
	if !strings.Contains(err.Error(), "failed_attempts must be a string or string array") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildHelpMePromptDoesNotEmitModeSection(t *testing.T) {
	prompt := buildHelpMePrompt(helpMePromptInput{
		Reason:       "parent may be stuck",
		OriginalGoal: "finish the design",
		Ask:          "re-evaluate the handoff",
	})
	if strings.Contains(prompt, "## Mode") {
		t.Fatalf("HelpMe prompt should not emit a Mode section:\n%s", prompt)
	}
	for _, want := range []string{"HelpMe Handoff Brief", "Why this handoff is needed", "User goal", "Task to complete"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("HelpMe prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, unwanted := range []string{"wrong assumption", "polluted context", "do not inherit", "fresh general-purpose helper agent"} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("HelpMe prompt should not expose internal recovery diagnosis %q:\n%s", unwanted, prompt)
		}
	}
}

// TestBuildHelpMePromptTeachesFinalMessageContract locks the completion
// contract wording: the helpme_recovery worker type owns the agent_report
// requirement at the runtime layer, so the task prompt no longer begs for a
// report and instead states that the final message is the deliverable.
func TestBuildHelpMePromptTeachesFinalMessageContract(t *testing.T) {
	prompt := buildHelpMePrompt(helpMePromptInput{
		Reason:       "parent may be stuck",
		OriginalGoal: "finish the design",
		Ask:          "re-evaluate the handoff",
	})
	if strings.Contains(prompt, "agent_report") {
		t.Fatalf("HelpMe prompt must not plead for agent_report; the worker type enforces it:\n%s", prompt)
	}
	if !strings.Contains(prompt, "final message is the deliverable") {
		t.Fatalf("HelpMe prompt should teach the final-message contract:\n%s", prompt)
	}
}

func TestWriteHelpMeMainTraceArchivesParentHistory(t *testing.T) {
	sessionDir := t.TempDir()
	ref, err := writeHelpMeMainTrace(&Env{
		SessionDir: sessionDir,
		AgentID:    "root-agent",
		AgentPath:  "/root",
	}, []providers.ChatMessage{
		{Role: "user", Content: "original task"},
		{Role: "assistant", Content: "wrong direction"},
	}, helpMeArgs{
		Reason:       "stuck",
		OriginalGoal: "original task",
	}, &agentcontrol.SpawnResult{
		AgentID:   "helper-1",
		AgentPath: "/root/helpme_recovery",
		Status:    "completed",
	}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref, "$SESSION_DIR/helpme/") {
		t.Fatalf("expected session ref, got %q", ref)
	}
	rel := strings.TrimPrefix(ref, "$SESSION_DIR/")
	data, err := os.ReadFile(filepath.Join(sessionDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	var payload struct {
		SchemaVersion string                  `json:"schema_version"`
		MainHistory   []providers.ChatMessage `json:"main_history"`
		ReportMissing bool                    `json:"report_missing"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode trace: %v", err)
	}
	if payload.SchemaVersion != "wuu/helpme-main-trace/v0.1" {
		t.Fatalf("schema = %q", payload.SchemaVersion)
	}
	if len(payload.MainHistory) != 2 || payload.MainHistory[0].Content != "original task" {
		t.Fatalf("main history not archived: %+v", payload.MainHistory)
	}
	if !payload.ReportMissing {
		t.Fatal("expected missing report to be recorded")
	}

	trace, traceRef, ok := readHelpMeMainTraceForAgent(sessionDir, "helper-1")
	if !ok {
		t.Fatal("expected trace lookup by helper id")
	}
	if trace.Args.OriginalGoal != "original task" {
		t.Fatalf("trace args not loaded: %+v", trace.Args)
	}
	if !strings.HasPrefix(traceRef, "$SESSION_DIR/helpme/") {
		t.Fatalf("expected session trace ref, got %q", traceRef)
	}
}

// helpMeFakeStreamClient replays one canned text turn per model call so
// spawned helpers complete immediately.
type helpMeFakeStreamClient struct{ content string }

func (f *helpMeFakeStreamClient) Chat(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{Content: f.content}, nil
}

func (f *helpMeFakeStreamClient) StreamChat(context.Context, providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	ch := make(chan providers.StreamEvent, 2)
	if f.content != "" {
		ch <- providers.StreamEvent{Type: providers.EventContentDelta, Content: f.content}
	}
	ch <- providers.StreamEvent{Type: providers.EventDone}
	close(ch)
	return ch, nil
}

type helpMeFakeToolkit struct{}

func (helpMeFakeToolkit) Definitions() []providers.ToolDefinition { return nil }
func (helpMeFakeToolkit) Execute(context.Context, providers.ToolCall) (string, error) {
	return "", nil
}

func newHelpMeTestControl(t *testing.T, dir, sessionDir string) *agentcontrol.AgentControl {
	t.Helper()
	return newHelpMeTestControlWithClient(t, dir, sessionDir, &helpMeFakeStreamClient{content: "helper looked with fresh eyes"})
}

func newHelpMeTestControlWithClient(t *testing.T, dir, sessionDir string, client providers.StreamClient) *agentcontrol.AgentControl {
	t.Helper()
	c, err := agentcontrol.New(agentcontrol.Config{
		Client:       client,
		DefaultModel: "fake-model",
		ParentRepo:   dir,
		WorktreeRoot: filepath.Join(dir, "wt"),
		SessionID:    "sess-helpme-tools",
		ThreadDir:    filepath.Join(sessionDir, "threads"),
		HarnessDir:   filepath.Join(sessionDir, "harness"),
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return helpMeFakeToolkit{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func executeHelpMe(t *testing.T, env *Env, argsJSON string) helpMeResponse {
	t.Helper()
	return executeHelpMeCtx(t, context.Background(), env, argsJSON)
}

func executeHelpMeCtx(t *testing.T, ctx context.Context, env *Env, argsJSON string) helpMeResponse {
	t.Helper()
	out, err := NewHelpMeTool(env).Execute(ctx, argsJSON)
	if err != nil {
		t.Fatalf("helpme execute: %v", err)
	}
	var response helpMeResponse
	if err := json.Unmarshal([]byte(out), &response); err != nil {
		t.Fatalf("decode helpme response: %v\n%s", err, out)
	}
	if response.AgentID == "" {
		t.Fatalf("helpme did not spawn a helper: %s", out)
	}
	return response
}

func waitForHelperReport(t *testing.T, c *agentcontrol.AgentControl, agentID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if reports, err := c.HarnessStore().ListReports(); err == nil {
			for _, report := range reports {
				if report.TaskID == agentID {
					return
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("helper %s never settled with a report", agentID)
}

// journalFakeStreamClient serves both the parent-journal extraction and the
// helper worker turns from one fake. Extraction requests are recognized by
// their dedicated system message ("decision journals"); they can succeed
// with canned journal content, fail, or block until the caller's timeout.
type journalFakeStreamClient struct {
	mu             sync.Mutex
	requests       []providers.ChatRequest
	journalContent string
	workerContent  string
	failJournal    bool
	blockJournal   bool
}

func (f *journalFakeStreamClient) isJournalRequest(req providers.ChatRequest) bool {
	return len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "decision journals")
}

func (f *journalFakeStreamClient) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()
	if f.isJournalRequest(req) {
		if f.failJournal {
			return providers.ChatResponse{}, errors.New("journal extraction backend down")
		}
		if f.blockJournal {
			<-ctx.Done()
			return providers.ChatResponse{}, ctx.Err()
		}
		return providers.ChatResponse{Content: f.journalContent}, nil
	}
	return providers.ChatResponse{Content: f.workerContent}, nil
}

func (f *journalFakeStreamClient) StreamChat(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	resp, err := f.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	ch := make(chan providers.StreamEvent, 2)
	if resp.Content != "" {
		ch <- providers.StreamEvent{Type: providers.EventContentDelta, Content: resp.Content}
	}
	ch <- providers.StreamEvent{Type: providers.EventDone}
	close(ch)
	return ch, nil
}

func (f *journalFakeStreamClient) recordedRequests() []providers.ChatRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]providers.ChatRequest(nil), f.requests...)
}

const cannedParentJournal = "### User goal\n\"fix the flaky login test\"\n\n### Paths taken\n- patched the router guard - ruled out"

var journalTestHistory = []providers.ChatMessage{
	{Role: "user", Content: "fix the flaky login test"},
	{Role: "assistant", Content: "I patched the router guard but the test still fails."},
}

// TestBuildHelpMePromptRendersParentJournal locks the handoff brief shape:
// the machine-extracted journal renders as its own section with the
// negative-knowledge framing.
func TestBuildHelpMePromptRendersParentJournal(t *testing.T) {
	prompt := buildHelpMePrompt(helpMePromptInput{
		Reason:                 "stuck",
		OriginalGoal:           "fix the flaky login test",
		ParentExecutionJournal: cannedParentJournal,
		Ask:                    "find the real cause",
	})
	for _, want := range []string{"## Parent execution journal", "Machine-extracted", "ruled out", "patched the router guard"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("HelpMe prompt missing journal material %q:\n%s", want, prompt)
		}
	}
	if !strings.Contains(buildHelpMePrompt(helpMePromptInput{OriginalGoal: "g", Ask: "a"}), "User goal") {
		t.Fatal("sanity: prompt builder broke without journal")
	}
	if strings.Contains(buildHelpMePrompt(helpMePromptInput{OriginalGoal: "g", Ask: "a"}), "Parent execution journal") {
		t.Fatal("journal section must not render when the extraction was skipped")
	}
}

// TestHelpMeExecuteExtractsAndAppliesParentJournal drives the double-source
// chain end to end: the helpme call machine-extracts the parent journal via
// the worker runtime's default client, feeds it to the helper's handoff
// brief, persists it on the recovery object, and the await-side joint
// compact renders it as the primary parent-side record.
func TestHelpMeExecuteExtractsAndAppliesParentJournal(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "session")
	client := &journalFakeStreamClient{journalContent: cannedParentJournal, workerContent: "helper found the real cause"}
	c := newHelpMeTestControlWithClient(t, dir, sessionDir, client)
	env := &Env{AgentControl: c, SessionDir: sessionDir}

	ctx := agent.ContextWithHistory(context.Background(), journalTestHistory)
	response := executeHelpMeCtx(t, ctx, env, `{"reason":"two failed attempts","ask":"find the real cause"}`)

	// (a) The recovery object carries and persists the journal.
	rec, ok := c.HelpMeRecoveryForHelper(response.AgentID)
	if !ok || !strings.Contains(rec.ParentExecutionJournal, "ruled out") {
		t.Fatalf("recovery missing parent journal: %+v ok=%v", rec, ok)
	}
	data, err := os.ReadFile(filepath.Join(sessionDir, "harness", "helpme-recovery", response.AgentID+".json"))
	if err != nil {
		t.Fatalf("read persisted recovery: %v", err)
	}
	if !strings.Contains(string(data), "parent_execution_journal") || !strings.Contains(string(data), "ruled out") {
		t.Fatalf("persisted recovery missing journal:\n%s", data)
	}

	// Let the async helper run settle so its model requests are recorded.
	waitForHelperReport(t, c, response.AgentID)

	// (b) The helper's handoff brief carries the journal section.
	var spawnPrompt string
	for _, req := range client.recordedRequests() {
		for _, msg := range req.Messages {
			if msg.Role == "user" && strings.Contains(msg.Content, "# HelpMe Handoff Brief") {
				spawnPrompt = msg.Content
			}
		}
	}
	if spawnPrompt == "" {
		t.Fatal("helper spawn prompt not observed")
	}
	for _, want := range []string{"## Parent execution journal", "patched the router guard - ruled out"} {
		if !strings.Contains(spawnPrompt, want) {
			t.Fatalf("helper brief missing journal %q:\n%s", want, spawnPrompt)
		}
	}
	// The resolved goal came from history, proving extraction did not
	// displace the existing brief resolution.
	if !strings.Contains(spawnPrompt, "fix the flaky login test") {
		t.Fatalf("helper brief lost the history-resolved goal:\n%s", spawnPrompt)
	}

	// (c) The await-side joint compact renders the journal as primary.
	if _, err := c.RecordAgentReport(response.AgentID, response.AgentPath, agentcontrol.AgentReportRequest{
		Outcome: "completed",
		Summary: "real bug was token refresh ordering",
	}); err != nil {
		t.Fatalf("RecordAgentReport: %v", err)
	}
	out, err := NewAwaitAgentsTool(env).Execute(context.Background(), `{"targets":["`+response.AgentID+`"]}`)
	if err != nil {
		t.Fatalf("await: %v", err)
	}
	var awaited awaitAgentsToolResponse
	if err := json.Unmarshal([]byte(out), &awaited); err != nil {
		t.Fatalf("decode await: %v\n%s", err, out)
	}
	if awaited.HistoryRewrite == nil {
		t.Fatalf("await must carry the rewrite:\n%s", out)
	}
	content := awaited.HistoryRewrite.Content
	journalIdx := strings.Index(content, "## Parent execution journal (machine-extracted)")
	supplementaryIdx := strings.Index(content, "## Parent self-reported brief (supplementary)")
	if journalIdx < 0 || supplementaryIdx < 0 || journalIdx > supplementaryIdx {
		t.Fatalf("joint compact must render the journal before the demoted self-report:\n%s", content)
	}
	if !strings.Contains(content, "patched the router guard - ruled out") {
		t.Fatalf("joint compact lost journal content:\n%s", content)
	}
}

// TestHelpMeExecuteDegradesWhenJournalExtractionFails locks the degrade
// contract: an extraction error never fails the helpme call; the rescue
// falls back to the resolved self-reported brief with no journal anywhere.
func TestHelpMeExecuteDegradesWhenJournalExtractionFails(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "session")
	client := &journalFakeStreamClient{failJournal: true, workerContent: "helper still ran"}
	c := newHelpMeTestControlWithClient(t, dir, sessionDir, client)
	env := &Env{AgentControl: c, SessionDir: sessionDir}

	ctx := agent.ContextWithHistory(context.Background(), journalTestHistory)
	response := executeHelpMeCtx(t, ctx, env, `{"reason":"stuck","ask":"find the real cause"}`)
	rec, ok := c.HelpMeRecoveryForHelper(response.AgentID)
	if !ok {
		t.Fatal("recovery must still register when extraction fails")
	}
	if rec.ParentExecutionJournal != "" {
		t.Fatalf("failed extraction must leave the journal empty, got %q", rec.ParentExecutionJournal)
	}
	if rec.Brief.OriginalGoal != "fix the flaky login test" {
		t.Fatalf("degraded rescue must keep the resolved brief, got %+v", rec.Brief)
	}
}

// TestHelpMeExecuteDegradesWhenJournalExtractionTimesOut proves the hard
// wall-clock bound: a hanging extraction backend delays the call by at most
// the timeout and then degrades, instead of wedging the rescue.
func TestHelpMeExecuteDegradesWhenJournalExtractionTimesOut(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "session")
	client := &journalFakeStreamClient{blockJournal: true, workerContent: "helper still ran"}
	c := newHelpMeTestControlWithClient(t, dir, sessionDir, client)
	env := &Env{AgentControl: c, SessionDir: sessionDir}

	restore := helpMeJournalTimeout
	helpMeJournalTimeout = 50 * time.Millisecond
	t.Cleanup(func() { helpMeJournalTimeout = restore })

	ctx := agent.ContextWithHistory(context.Background(), journalTestHistory)
	start := time.Now()
	response := executeHelpMeCtx(t, ctx, env, `{"reason":"stuck","ask":"find the real cause"}`)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("helpme blocked far beyond the journal timeout: %v", elapsed)
	}
	rec, ok := c.HelpMeRecoveryForHelper(response.AgentID)
	if !ok || rec.ParentExecutionJournal != "" {
		t.Fatalf("timed-out extraction must degrade to an empty journal, got %+v ok=%v", rec, ok)
	}
}

// TestHelpMeExecuteSurvivesTraceWriteFailure locks the audit downgrade: when
// the session directory cannot hold the main trace, the call still succeeds
// (the helper is already spawned) and only annotates the missing trace.
func TestHelpMeExecuteSurvivesTraceWriteFailure(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "session")
	c := newHelpMeTestControl(t, dir, sessionDir)

	// A regular file where the session dir should be makes MkdirAll fail.
	blockedSessionDir := filepath.Join(dir, "session-as-file")
	if err := os.WriteFile(blockedSessionDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := &Env{AgentControl: c, SessionDir: blockedSessionDir}

	response := executeHelpMe(t, env, `{"reason":"stuck","original_goal":"fix the login flow","ask":"find the real cause"}`)
	if response.MainTracePath != "" {
		t.Fatalf("trace write failure must leave main_trace_path empty, got %q", response.MainTracePath)
	}
	joined := strings.Join(response.NextSteps, "\n")
	if !strings.Contains(joined, "Audit trace could not be written") {
		t.Fatalf("expected next_steps to note the missing audit trace, got %+v", response.NextSteps)
	}
	// The recovery state was still registered, so the rescue stays usable.
	if _, ok := c.HelpMeRecoveryForHelper(response.AgentID); !ok {
		t.Fatal("recovery must be registered even when the audit trace fails")
	}
}

// TestHelpMeAwaitRewriteIsOneShot drives the full product path: helpme spawns
// a helpme_recovery worker, the helper completes with a structured report,
// the first await_agents call returns the joint-compact history rewrite built
// from the registered recovery brief, and a second await returns the result
// again but never a second rewrite.
func TestHelpMeAwaitRewriteIsOneShot(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "session")
	c := newHelpMeTestControl(t, dir, sessionDir)
	env := &Env{AgentControl: c, SessionDir: sessionDir}

	response := executeHelpMe(t, env, `{"reason":"two failed attempts","original_goal":"fix the login flow","ask":"find the real cause"}`)
	if !strings.HasPrefix(response.MainTracePath, "$SESSION_DIR/helpme/") {
		t.Fatalf("expected audit trace ref, got %q", response.MainTracePath)
	}

	// Let the run settle (the requires_report closing turn synthesizes a
	// final_text report when the fake model files nothing), then file the
	// structured report the rewrite depends on.
	waitForHelperReport(t, c, response.AgentID)
	if _, err := c.RecordAgentReport(response.AgentID, response.AgentPath, agentcontrol.AgentReportRequest{
		Outcome: "completed",
		Summary: "real bug was token refresh ordering",
	}); err != nil {
		t.Fatalf("RecordAgentReport: %v", err)
	}

	await := NewAwaitAgentsTool(env)
	firstOut, err := await.Execute(context.Background(), `{"targets":["`+response.AgentID+`"]}`)
	if err != nil {
		t.Fatalf("first await: %v", err)
	}
	var first awaitAgentsToolResponse
	if err := json.Unmarshal([]byte(firstOut), &first); err != nil {
		t.Fatalf("decode first await: %v\n%s", err, firstOut)
	}
	if first.HistoryRewrite == nil {
		t.Fatalf("first await must carry the HelpMe history rewrite:\n%s", firstOut)
	}
	if first.HistoryRewrite.Kind != compact.HelpMeHistoryRewriteKind {
		t.Fatalf("unexpected rewrite kind %q", first.HistoryRewrite.Kind)
	}
	// The compact is built from the resolved recovery brief, not a
	// placeholder goal.
	if !strings.Contains(first.HistoryRewrite.Content, "fix the login flow") {
		t.Fatalf("rewrite lost the resolved user goal:\n%s", first.HistoryRewrite.Content)
	}
	if !strings.Contains(first.HistoryRewrite.Content, "token refresh ordering") {
		t.Fatalf("rewrite lost the helper report summary:\n%s", first.HistoryRewrite.Content)
	}

	secondOut, err := await.Execute(context.Background(), `{"targets":["`+response.AgentID+`"]}`)
	if err != nil {
		t.Fatalf("second await: %v", err)
	}
	var second awaitAgentsToolResponse
	if err := json.Unmarshal([]byte(secondOut), &second); err != nil {
		t.Fatalf("decode second await: %v\n%s", err, secondOut)
	}
	if second.HistoryRewrite != nil {
		t.Fatalf("second await must not rewrite history again:\n%s", secondOut)
	}
	if len(second.Results) != 1 || second.Results[0].AgentID != response.AgentID {
		t.Fatalf("second await must still return the helper result: %+v", second.Results)
	}
	// And the recovery is now consumed for good.
	rec, ok := c.HelpMeRecoveryForHelper(response.AgentID)
	if !ok || !rec.Applied {
		t.Fatalf("recovery must be marked applied after the first rewrite, got %+v ok=%v", rec, ok)
	}
}

func TestAppendHelpMeAwaitGuidanceWarnsAgainstRawTranscriptMerge(t *testing.T) {
	result := agentcontrol.AwaitAgentsResult{
		Action: "await_agents",
		Results: []agentcontrol.AwaitAgentResult{{
			AgentID:    "helper-1",
			TaskName:   "helpme_recovery_abc123",
			AgentPath:  "/root/helpme_recovery_abc123",
			Status:     "completed",
			ReportPath: "$SESSION_DIR/harness/reports/helper-1.md",
		}},
	}

	appendHelpMeAwaitGuidance(&result, nil)

	joined := strings.Join(result.NextSteps, "\n")
	for _, want := range []string{"agent_report", "main_trace_path", "do not paste or merge raw parent/helper transcripts", "inception"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("HelpMe await guidance missing %q:\n%s", want, joined)
		}
	}
}
