package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/appserver"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/evalharness"
	wuuexec "github.com/blueberrycongee/wuu/internal/exec"
	goalrunner "github.com/blueberrycongee/wuu/internal/goal"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/sessiontrace"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/tools"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

func TestRunVersionAliasForwardsJSONFlag(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"--version", "--json"}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	var payload map[string]any
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("expected JSON output, got %q: %v", output, err)
	}
	if _, ok := payload["version"]; !ok {
		t.Fatalf("expected version field in JSON output: %v", payload)
	}
}

func TestRunVersionAliasForwardsLongFlag(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"-v", "--long"}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	if !strings.Contains(output, "version:") {
		t.Fatalf("expected long version output, got %q", output)
	}
	if !strings.Contains(output, "commit:") {
		t.Fatalf("expected long version output to include commit, got %q", output)
	}
}

func TestRunWithoutArgsPrintsUsage(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run(nil); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})
	if !strings.Contains(output, "GUI-first") || strings.Contains(output, "wuu tui") {
		t.Fatalf("unexpected usage output: %q", output)
	}
}

func TestRunTUICommandIsRemoved(t *testing.T) {
	err := run([]string{"tui"})
	if err == nil || !strings.Contains(err.Error(), "TUI has been removed") {
		t.Fatalf("expected removed TUI error, got %v", err)
	}
}

func TestRunExecJSONUsesControllerPath(t *testing.T) {
	controller := newCLIExecFakeController(
		cliExecNotification(appserver.NotificationAgentMessageDelta, appserver.AgentMessageDeltaNotification{ThreadID: "thread-1", TurnID: "turn-1", Delta: "ok"}),
		cliExecNotification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "ok"}),
	)
	restore := installExecControllerOverride(t, controller)
	defer restore()

	output := captureStdout(t, func() {
		if err := run([]string{"exec", "--json", "hello"}); err != nil {
			t.Fatalf("run exec: %v", err)
		}
	})

	if !controller.startedThread || controller.startedPrompt != "hello" {
		t.Fatalf("exec did not use expected controller path: %+v", controller)
	}
	events := parseCLIJSONLines(t, output)
	if got := events[0]["type"]; got != "session_configured" {
		t.Fatalf("first event = %v, want session_configured", got)
	}
	if got := events[len(events)-1]["type"]; got != "result" {
		t.Fatalf("last event = %v, want result", got)
	}
}

func TestRunExecResumeLastUsesResumePath(t *testing.T) {
	controller := newCLIExecFakeController(
		cliExecNotification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "continued"}),
	)
	restore := installExecControllerOverride(t, controller)
	defer restore()

	output := captureStdout(t, func() {
		if err := run([]string{"exec", "resume", "--last", "--json", "continue"}); err != nil {
			t.Fatalf("run exec resume: %v", err)
		}
	})

	if controller.startedThread {
		t.Fatal("resume should not start a new thread")
	}
	if controller.resumedThread != "" {
		t.Fatalf("resume --last should pass empty thread id, got %q", controller.resumedThread)
	}
	events := parseCLIJSONLines(t, output)
	if got := events[1]["type"]; got != "thread_resumed" {
		t.Fatalf("second event = %v, want thread_resumed\n%s", got, output)
	}
}

func TestRunExecResumeAllUsesResumePath(t *testing.T) {
	controller := newCLIExecFakeController(
		cliExecNotification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "continued"}),
	)
	restore := installExecControllerOverride(t, controller)
	defer restore()

	output := captureStdout(t, func() {
		if err := run([]string{"exec", "resume", "--all", "--json", "thread-1", "continue"}); err != nil {
			t.Fatalf("run exec resume --all: %v", err)
		}
	})

	if controller.startedThread || controller.resumedThread != "thread-1" {
		t.Fatalf("resume --all should resume thread-1, got started=%v resumed=%q", controller.startedThread, controller.resumedThread)
	}
	events := parseCLIJSONLines(t, output)
	if got := events[1]["type"]; got != "thread_resumed" {
		t.Fatalf("second event = %v, want thread_resumed\n%s", got, output)
	}
}

func TestRunExecForkUsesForkPath(t *testing.T) {
	controller := newCLIExecFakeController(
		cliExecNotification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "fork-thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "forked"}),
	)
	restore := installExecControllerOverride(t, controller)
	defer restore()

	output := captureStdout(t, func() {
		if err := run([]string{"exec", "fork", "--json", "source-thread", "continue"}); err != nil {
			t.Fatalf("run exec fork: %v", err)
		}
	})

	if controller.startedThread || controller.resumedThread != "" {
		t.Fatalf("fork should not start or resume: started=%v resumed=%q", controller.startedThread, controller.resumedThread)
	}
	if controller.forkedThread != "source-thread" {
		t.Fatalf("forkedThread = %q", controller.forkedThread)
	}
	if controller.startedPrompt != "continue" {
		t.Fatalf("prompt = %q", controller.startedPrompt)
	}
	events := parseCLIJSONLines(t, output)
	if got := events[1]["type"]; got != "thread_forked" {
		t.Fatalf("second event = %v, want thread_forked\n%s", got, output)
	}
}

func TestRunExecEphemeralUsesEphemeralStart(t *testing.T) {
	controller := newCLIExecFakeController(
		cliExecNotification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "done"}),
	)
	restore := installExecControllerOverride(t, controller)
	defer restore()

	_ = captureStdout(t, func() {
		if err := run([]string{"exec", "--ephemeral", "--json", "scratch task"}); err != nil {
			t.Fatalf("run exec ephemeral: %v", err)
		}
	})

	if !controller.startedThread || !controller.startEphemeral {
		t.Fatalf("expected ephemeral thread start: %+v", controller)
	}
}

func TestRunExecPassesFileAndImageAttachments(t *testing.T) {
	workdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workdir, "shot.png"), []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "report.pdf"), []byte("%PDF-1.7\n"), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	controller := newCLIExecFakeController(
		cliExecNotification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "ok"}),
	)
	restore := installExecControllerOverride(t, controller)
	defer restore()

	_ = captureStdout(t, func() {
		if err := run([]string{"exec", "--workdir", workdir, "--file", "report.pdf", "--image", "shot.png", "inspect"}); err != nil {
			t.Fatalf("run exec: %v", err)
		}
	})

	if controller.startedPrompt != "inspect" {
		t.Fatalf("prompt = %q", controller.startedPrompt)
	}
	if len(controller.startedFiles) != 1 || controller.startedFiles[0].Filename != "report.pdf" || controller.startedFiles[0].MediaType != "application/pdf" {
		t.Fatalf("unexpected file attachments: %+v", controller.startedFiles)
	}
	if len(controller.startedImages) != 1 || controller.startedImages[0].MediaType != "image/png" {
		t.Fatalf("unexpected image attachments: %+v", controller.startedImages)
	}
	if controller.startedFiles[0].Data == "" || controller.startedImages[0].Data == "" {
		t.Fatalf("attachment data should be base64 encoded: files=%+v images=%+v", controller.startedFiles, controller.startedImages)
	}
}

func TestRunExecInputJSONUsesMachineInput(t *testing.T) {
	workdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workdir, "shot.png"), []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "report.pdf"), []byte("%PDF-1.7\n"), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	controller := newCLIExecFakeController(
		cliExecNotification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "ok"}),
	)
	restore := installExecControllerOverride(t, controller)
	defer restore()
	input := `{
		"prompt": "use this log",
		"stdin": "panic: boom",
		"workdir": "` + filepath.ToSlash(workdir) + `",
		"provider": "p",
		"model": "m",
		"json": true,
		"ephemeral": true,
		"files": ["report.pdf"],
		"images": ["shot.png"]
	}`

	output := withStdin(t, input, func() string {
		return captureStdout(t, func() {
			if err := run([]string{"exec", "--input-json"}); err != nil {
				t.Fatalf("run exec input JSON: %v", err)
			}
		})
	})

	if controller.startedPrompt != "use this log\n\n<stdin>\npanic: boom\n</stdin>" {
		t.Fatalf("prompt = %q", controller.startedPrompt)
	}
	if !controller.startEphemeral {
		t.Fatalf("expected ephemeral start: %+v", controller)
	}
	if len(controller.startedFiles) != 1 || controller.startedFiles[0].Filename != "report.pdf" {
		t.Fatalf("unexpected file attachments: %+v", controller.startedFiles)
	}
	if len(controller.startedImages) != 1 || controller.startedImages[0].MediaType != "image/png" {
		t.Fatalf("unexpected image attachments: %+v", controller.startedImages)
	}
	events := parseCLIJSONLines(t, output)
	if got := events[0]["type"]; got != "session_configured" {
		t.Fatalf("expected JSONL output from input JSON, got %v\n%s", got, output)
	}
}

func TestRunExecInputJSONRejectsPositionalPrompt(t *testing.T) {
	err := withStdin(t, `{"prompt":"hello"}`, func() error {
		return run([]string{"exec", "--input-json", "extra"})
	})
	if wuuexec.ExitCode(err) != wuuexec.ExitInvalidInput {
		t.Fatalf("ExitCode = %d, err=%v", wuuexec.ExitCode(err), err)
	}
	if err == nil || !strings.Contains(err.Error(), "positional prompt") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExecAllowsAttachmentOnlyPrompt(t *testing.T) {
	workdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workdir, "report.pdf"), []byte("%PDF-1.7\n"), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	controller := newCLIExecFakeController(
		cliExecNotification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "ok"}),
	)
	restore := installExecControllerOverride(t, controller)
	defer restore()

	_ = captureStdout(t, func() {
		if err := run([]string{"exec", "--workdir", workdir, "--file", "report.pdf"}); err != nil {
			t.Fatalf("run exec: %v", err)
		}
	})

	if controller.startedPrompt != "" {
		t.Fatalf("prompt = %q, want empty attachment-only prompt", controller.startedPrompt)
	}
	if len(controller.startedFiles) != 1 {
		t.Fatalf("file attachment missing: %+v", controller.startedFiles)
	}
}

func TestExecOptionsFromCLIAcceptsMaxTurns(t *testing.T) {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := addExecFlags(fs)
	if err := fs.Parse([]string{"--max-turns", "3"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	opts, err := execOptionsFromCLI(cfg, "hello", "", false, nil)
	if err != nil {
		t.Fatalf("execOptionsFromCLI: %v", err)
	}
	if opts.MaxTurns != 3 {
		t.Fatalf("MaxTurns = %d, want 3", opts.MaxTurns)
	}
}

func TestExecOptionsFromInputJSONAcceptsMaxTurns(t *testing.T) {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := addExecFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	maxTurns := 4
	opts, err := execOptionsFromCLI(cfg, "hello", "", false, &execInputPayload{MaxTurns: &maxTurns})
	if err != nil {
		t.Fatalf("execOptionsFromCLI: %v", err)
	}
	if opts.MaxTurns != 4 {
		t.Fatalf("MaxTurns = %d, want 4", opts.MaxTurns)
	}
}

func TestExecOptionsFromCLIAcceptsOutputSchema(t *testing.T) {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := addExecFlags(fs)
	if err := fs.Parse([]string{"--output-schema", "schema.json"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	opts, err := execOptionsFromCLI(cfg, "hello", "", false, nil)
	if err != nil {
		t.Fatalf("execOptionsFromCLI: %v", err)
	}
	if opts.OutputSchemaPath != "schema.json" {
		t.Fatalf("OutputSchemaPath = %q, want schema.json", opts.OutputSchemaPath)
	}
}

func TestExecOptionsFromInputJSONAcceptsOutputSchema(t *testing.T) {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := addExecFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	opts, err := execOptionsFromCLI(cfg, "hello", "", false, &execInputPayload{OutputSchema: "schema.json"})
	if err != nil {
		t.Fatalf("execOptionsFromCLI: %v", err)
	}
	if opts.OutputSchemaPath != "schema.json" {
		t.Fatalf("OutputSchemaPath = %q, want schema.json", opts.OutputSchemaPath)
	}
}

func TestExecOptionsFromCLIAcceptsApprovalAndToolOverrides(t *testing.T) {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := addExecFlags(fs)
	if err := fs.Parse([]string{
		"--approval-handler", "approve-tool",
		"--allow-tool", "run_shell",
		"--deny-tool", "write_file",
	}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	opts, err := execOptionsFromCLI(cfg, "hello", "", false, nil)
	if err != nil {
		t.Fatalf("execOptionsFromCLI: %v", err)
	}
	if opts.ApprovalHandler != "approve-tool" {
		t.Fatalf("ApprovalHandler = %q", opts.ApprovalHandler)
	}
	if !reflect.DeepEqual(opts.AllowTools, []string{"run_shell"}) || !reflect.DeepEqual(opts.DenyTools, []string{"write_file"}) {
		t.Fatalf("tool overrides = allow %+v deny %+v", opts.AllowTools, opts.DenyTools)
	}
}

func TestExecOptionsFromInputJSONAcceptsApprovalSocketAndToolOverrides(t *testing.T) {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := addExecFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	opts, err := execOptionsFromCLI(cfg, "hello", "", false, &execInputPayload{
		ApprovalSocket: "/tmp/wuu-approval.sock",
		AllowTools:     []string{"read_file"},
		DenyTools:      []string{"run_shell"},
	})
	if err != nil {
		t.Fatalf("execOptionsFromCLI: %v", err)
	}
	if opts.ApprovalSocket != "/tmp/wuu-approval.sock" {
		t.Fatalf("ApprovalSocket = %q", opts.ApprovalSocket)
	}
	if !reflect.DeepEqual(opts.AllowTools, []string{"read_file"}) || !reflect.DeepEqual(opts.DenyTools, []string{"run_shell"}) {
		t.Fatalf("tool overrides = allow %+v deny %+v", opts.AllowTools, opts.DenyTools)
	}
}

func TestRunExecRejectsConflictingToolOverrides(t *testing.T) {
	err := run([]string{"exec", "--allow-tool", "run_shell", "--deny-tool", "run_shell", "hello"})
	if wuuexec.ExitCode(err) != wuuexec.ExitInvalidInput {
		t.Fatalf("ExitCode = %d, err=%v", wuuexec.ExitCode(err), err)
	}
	if err == nil || !strings.Contains(err.Error(), "both allowed and denied") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExecRejectsConflictingApprovalHandlers(t *testing.T) {
	err := run([]string{"exec", "--approval-handler", "approve", "--approval-socket", "/tmp/wuu.sock", "hello"})
	if wuuexec.ExitCode(err) != wuuexec.ExitInvalidInput {
		t.Fatalf("ExitCode = %d, err=%v", wuuexec.ExitCode(err), err)
	}
	if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExecRejectsNegativeMaxTurnsWithExitCodeTwo(t *testing.T) {
	err := run([]string{"exec", "--max-turns=-1", "hello"})
	if wuuexec.ExitCode(err) != wuuexec.ExitInvalidInput {
		t.Fatalf("ExitCode = %d, err=%v", wuuexec.ExitCode(err), err)
	}
	if err == nil || !strings.Contains(err.Error(), "--max-turns must be non-negative") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExecRejectsInvalidEnvWithExitCodeTwo(t *testing.T) {
	err := run([]string{"exec", "--env", "not-an-assignment", "hello"})
	if wuuexec.ExitCode(err) != wuuexec.ExitInvalidInput {
		t.Fatalf("ExitCode = %d, err=%v", wuuexec.ExitCode(err), err)
	}
	if err == nil || !strings.Contains(err.Error(), "--env must be KEY=VALUE") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCommandForwardsToExecControllerPath(t *testing.T) {
	controller := newCLIExecFakeController(
		cliExecNotification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "run result"}),
	)
	restore := installExecControllerOverride(t, controller)
	defer restore()

	output := captureStdout(t, func() {
		if err := run([]string{"run", "--json", "hello from legacy run"}); err != nil {
			t.Fatalf("run legacy run: %v", err)
		}
	})

	if !controller.startedThread || controller.startedPrompt != "hello from legacy run" {
		t.Fatalf("run did not use expected exec controller path: %+v", controller)
	}
	events := parseCLIJSONLines(t, output)
	if got := events[len(events)-1]["type"]; got != "result" {
		t.Fatalf("last event = %v, want result\n%s", got, output)
	}
}

func TestRunCommandRejectsLegacyOnlyFlags(t *testing.T) {
	for _, args := range [][]string{
		{"run", "--max-steps", "3", "hello"},
		{"run", "--temperature=0.2", "hello"},
		{"run", "--system-prompt", "be terse", "hello"},
	} {
		err := run(args)
		if wuuexec.ExitCode(err) != wuuexec.ExitInvalidInput {
			t.Fatalf("ExitCode(%v) = %d, err=%v", args, wuuexec.ExitCode(err), err)
		}
		if err == nil || !strings.Contains(err.Error(), "compatibility wrapper around wuu exec") {
			t.Fatalf("unexpected error for %v: %v", args, err)
		}
	}
}

func TestRunExecReviewUsesExecControllerPath(t *testing.T) {
	controller := newCLIExecFakeController(
		cliExecNotification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "reviewed"}),
	)
	restore := installExecControllerOverride(t, controller)
	defer restore()

	output := captureStdout(t, func() {
		if err := run([]string{"exec", "review", "--uncommitted", "--json", "prioritize tests"}); err != nil {
			t.Fatalf("run exec review: %v", err)
		}
	})

	if !controller.startedThread {
		t.Fatalf("review should start an exec thread: %+v", controller)
	}
	if !strings.Contains(controller.startedPrompt, "Review the current uncommitted changes") ||
		!strings.Contains(controller.startedPrompt, "git diff") ||
		!strings.Contains(controller.startedPrompt, "prioritize tests") {
		t.Fatalf("unexpected review prompt: %q", controller.startedPrompt)
	}
	events := parseCLIJSONLines(t, output)
	if got := events[len(events)-1]["type"]; got != "result" {
		t.Fatalf("last event = %v, want result\n%s", got, output)
	}
}

func TestRunExecReviewRequiresOneScope(t *testing.T) {
	for _, args := range [][]string{
		{"exec", "review"},
		{"exec", "review", "--uncommitted", "--base", "main"},
		{"exec", "review", "--base", "main", "--commit", "abc123"},
	} {
		err := run(args)
		if wuuexec.ExitCode(err) != wuuexec.ExitInvalidInput {
			t.Fatalf("ExitCode(%v) = %d, err=%v", args, wuuexec.ExitCode(err), err)
		}
		if err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Fatalf("unexpected review error for %v: %v", args, err)
		}
	}
}

func TestRunInitWritesDefaultConfig(t *testing.T) {
	workdir := t.TempDir()
	t.Chdir(workdir)
	output := captureStdout(t, func() {
		if err := run([]string{"init", "--force"}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})
	if !strings.Contains(output, "created") {
		t.Fatalf("expected created output, got %q", output)
	}
	data, err := os.ReadFile(filepath.Join(workdir, ".wuu.json"))
	if err != nil {
		t.Fatalf("expected config file: %v", err)
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("expected JSON config: %v", err)
	}
	if cfg.DefaultProvider == "" || len(cfg.Providers) == 0 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestRunGoalDemoWritesDurableState(t *testing.T) {
	workdir := t.TempDir()
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	t.Setenv("WUU_HOME", wuuHome)
	if err := os.WriteFile(filepath.Join(workdir, "marker.txt"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := run([]string{
			"goal", "demo",
			"--workdir", workdir,
			"--id", "goal-test",
			"--goal", "prove durable goal",
			"--verify-command", "test -f marker.txt",
		}); err != nil {
			t.Fatalf("run goal demo: %v", err)
		}
	})
	if !strings.Contains(output, "status: completed") {
		t.Fatalf("expected completed summary, got %q", output)
	}
	goalDir := cliGoalDir(t, wuuHome, workdir, "goal-test")
	for _, rel := range []string{
		"state.json",
		"events.jsonl",
		"views/progress.md",
		"views/decisions.md",
		"views/failures.md",
		"views/approvals.md",
		"artifacts/research.md",
		"artifacts/plan.md",
		"artifacts/todo.md",
		"artifacts/verification.md",
		"artifacts/review.md",
		"artifacts/integration.md",
		"artifacts/final.md",
	} {
		if _, err := os.Stat(filepath.Join(goalDir, rel)); err != nil {
			t.Fatalf("expected %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(workdir, ".goal")); !os.IsNotExist(err) {
		t.Fatalf("project root .goal should not be created, stat err=%v", err)
	}

	status := captureStdout(t, func() {
		if err := run([]string{"goal", "status", "--workdir", workdir, "--id", "goal-test", "--json"}); err != nil {
			t.Fatalf("run goal status: %v", err)
		}
	})
	var state struct {
		ID          string `json:"id"`
		Status      string `json:"status"`
		Final       string `json:"final_artifact"`
		TestResults []struct {
			Passed bool `json:"passed"`
		} `json:"test_results"`
	}
	if err := json.Unmarshal([]byte(status), &state); err != nil {
		t.Fatalf("parse goal status JSON: %v\n%s", err, status)
	}
	if state.ID != "goal-test" || state.Status != "completed" || state.Final == "" {
		t.Fatalf("unexpected goal state: %+v", state)
	}
	if len(state.TestResults) != 1 || !state.TestResults[0].Passed {
		t.Fatalf("expected one passing test result, got %+v", state.TestResults)
	}
}

func TestRunGoalDemoRecordsVerificationFailure(t *testing.T) {
	workdir := t.TempDir()
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	t.Setenv("WUU_HOME", wuuHome)

	output := captureStdout(t, func() {
		if err := run([]string{
			"goal", "demo",
			"--workdir", workdir,
			"--id", "goal-fail",
			"--goal", "capture failure",
			"--verify-command", "test -f missing.txt",
		}); err != nil {
			t.Fatalf("run goal demo: %v", err)
		}
	})
	if !strings.Contains(output, "status: needs_human") {
		t.Fatalf("expected needs_human summary, got %q", output)
	}
	goalDir := cliGoalDir(t, wuuHome, workdir, "goal-fail")
	failures, err := os.ReadFile(filepath.Join(goalDir, "views", "failures.md"))
	if err != nil {
		t.Fatalf("read failures: %v", err)
	}
	if !strings.Contains(string(failures), "verification_command_failed") {
		t.Fatalf("expected verification failure, got %s", failures)
	}
	if _, err := os.Stat(filepath.Join(workdir, ".goal")); !os.IsNotExist(err) {
		t.Fatalf("project root .goal should not be created, stat err=%v", err)
	}
}

func cliGoalDir(t *testing.T, wuuHome, workdir, goalID string) string {
	t.Helper()
	workspaceStateDir, err := statepath.WorkspaceDir(wuuHome, workdir)
	if err != nil {
		t.Fatalf("WorkspaceDir: %v", err)
	}
	return statepath.GoalDir(workspaceStateDir, goalID)
}

func TestLoadOrCreateAppServerConfigCreatesStarterConfig(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()

	cfg, configPath, err := loadOrCreateAppServerConfig(root, home)
	if err != nil {
		t.Fatalf("loadOrCreateAppServerConfig: %v", err)
	}

	expectedPath := filepath.Join(home, ".config", "wuu", "config.json")
	if configPath != expectedPath {
		t.Fatalf("expected config path %q, got %q", expectedPath, configPath)
	}
	if cfg.DefaultProvider != "openai-codex" {
		t.Fatalf("expected openai-codex default provider, got %q", cfg.DefaultProvider)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("expected one starter provider, got %+v", cfg.Providers)
	}
	if _, ok := cfg.Providers["openai-codex"]; !ok {
		t.Fatalf("starter config missing openai-codex provider: %+v", cfg.Providers)
	}

	loaded, loadedPath, err := config.LoadFrom(root, home)
	if err != nil {
		t.Fatalf("reload starter config: %v", err)
	}
	if loadedPath != configPath || loaded.DefaultProvider != "openai-codex" {
		t.Fatalf("unexpected reloaded config: path=%q cfg=%+v", loadedPath, loaded)
	}
}

func TestRunModelsRejectsUnsupportedProvider(t *testing.T) {
	workdir := t.TempDir()
	configPath := workdir + "/.wuu.json"
	data := `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://example.com/v1",
      "api_key": "sk-test",
      "model": "gpt-test"
    }
  },
  "agent": {
    "system_prompt": "test"
  }
}`
	if err := os.WriteFile(configPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	err := run([]string{"models", "--workdir", workdir})
	if err == nil {
		t.Fatal("expected unsupported provider error")
	}
	if !strings.Contains(err.Error(), "openai-codex providers only") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunEvalListDoesNotRequireConfig(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run([]string{"eval", "--list"}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	if !strings.Contains(output, "test_failure_fix") ||
		!strings.Contains(output, "git_test_failure_fix") ||
		!strings.Contains(output, "multi_file_pricing") ||
		!strings.Contains(output, "long_process_output") ||
		!strings.Contains(output, "tool_search_deferred") ||
		!strings.Contains(output, "stale_read_guard") ||
		!strings.Contains(output, "mcp_readonly_concurrency") ||
		!strings.Contains(output, "mcp_live_discovery") ||
		!strings.Contains(output, "multi_agent_worker") ||
		!strings.Contains(output, "checkpoint_rollback") {
		t.Fatalf("expected built-in eval tasks, got %q", output)
	}
}

func TestResolveEvalTasksSelectsCommaSeparatedIDs(t *testing.T) {
	tasks, err := resolveEvalTasks("test_failure_fix,multi_file_pricing")
	if err != nil {
		t.Fatalf("resolveEvalTasks: %v", err)
	}
	if len(tasks) != 2 || tasks[0].ID != "test_failure_fix" || tasks[1].ID != "multi_file_pricing" {
		t.Fatalf("unexpected tasks: %+v", tasks)
	}
}

func TestResolveEvalTasksRejectsUnknownTask(t *testing.T) {
	_, err := resolveEvalTasks("missing")
	if err == nil || !strings.Contains(err.Error(), "unknown eval task") {
		t.Fatalf("expected unknown task error, got %v", err)
	}
}

func TestRunEvalLiveCodexOAuthSkipsWithoutCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex-missing"))

	output := captureStdout(t, func() {
		if err := run([]string{"eval", "--live-codex-oauth"}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	if !strings.Contains(output, "SKIP live Codex OAuth eval") {
		t.Fatalf("expected skip output, got %q", output)
	}
}

func TestMissingRequiredTools(t *testing.T) {
	missing := missingRequiredTools([]string{"tool_search", "list_cron"}, []string{"tool_search", "write_file"})
	if len(missing) != 1 || missing[0] != "list_cron" {
		t.Fatalf("unexpected missing tools: %+v", missing)
	}
	if got := missingRequiredTools([]string{"tool_search"}, []string{"tool_search"}); len(got) != 0 {
		t.Fatalf("expected no missing tools, got %+v", got)
	}

	forbidden := forbiddenToolsUsed([]string{"create_workflow", "run_workflow"}, []string{"start_workflow", "create_workflow", "create_workflow"})
	if len(forbidden) != 1 || forbidden[0] != "create_workflow" {
		t.Fatalf("unexpected forbidden tools: %+v", forbidden)
	}
}

func TestToolNameSequencePreservesRepeatedCalls(t *testing.T) {
	got := toolNameSequence([]tools.ToolExecutionRecord{
		{Name: "read_file"},
		{Name: "checkpoint"},
		{Name: "apply_patch"},
		{Name: "checkpoint"},
		{Name: "apply_patch"},
	})
	want := []string{"read_file", "checkpoint", "apply_patch", "checkpoint", "apply_patch"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tool sequence mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestMissingRequiredToolErrors(t *testing.T) {
	required := []evalharness.ToolErrorRequirement{
		{ToolName: "edit_file", ErrorContains: "changed since last read"},
	}
	records := []tools.ToolExecutionRecord{
		{Name: "edit_file", Success: false, Error: "file changed since last read. Use read_file again before editing"},
	}
	if got := missingRequiredToolErrors(required, records); len(got) != 0 {
		t.Fatalf("expected no missing errors, got %+v", got)
	}

	missing := missingRequiredToolErrors(required, []tools.ToolExecutionRecord{
		{Name: "edit_file", Success: false, Error: "old_text not found"},
	})
	if len(missing) != 1 || missing[0] != "edit_file:changed since last read" {
		t.Fatalf("unexpected missing errors: %+v", missing)
	}
}

func TestMissingRequiredToolCalls(t *testing.T) {
	messages := []providers.ChatMessage{
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{Name: "checkpoint", Arguments: `{"action":"create","checkpoint_id":"before_bad_edit","paths":["target.txt","scratch.txt"]}`},
				{Name: "write_file", Arguments: `{"path":"checkpoint_result.txt","content":"CHECKPOINT_ROLLBACK_DONE\n"}`},
			},
		},
	}
	required := []evalharness.ToolCallRequirement{
		{ToolName: "checkpoint", ArgumentEquals: map[string]string{"action": "create", "checkpoint_id": "before_bad_edit"}, ArgsContains: []string{"scratch.txt"}},
		{ToolName: "write_file", ArgumentEquals: map[string]string{"path": "checkpoint_result.txt"}},
	}
	if got := missingRequiredToolCalls(required, messages); len(got) != 0 {
		t.Fatalf("expected no missing tool calls, got %+v", got)
	}

	missing := missingRequiredToolCalls([]evalharness.ToolCallRequirement{
		{ToolName: "checkpoint", ArgumentEquals: map[string]string{"action": "restore", "checkpoint_id": "before_bad_edit"}},
	}, messages)
	if len(missing) != 1 || missing[0] != "checkpoint action=restore checkpoint_id=before_bad_edit" {
		t.Fatalf("unexpected missing tool calls: %+v", missing)
	}
}

func TestMissingRequiredToolSequence(t *testing.T) {
	messages := []providers.ChatMessage{
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{Name: "checkpoint", Arguments: `{"action":"create","checkpoint_id":"before_bad_edit","paths":["target.txt","scratch.txt"]}`},
				{Name: "apply_patch", Arguments: "*** Update File: target.txt\n*** Add File: scratch.txt\n"},
			},
		},
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{Name: "checkpoint", Arguments: `{"action":"restore","checkpoint_id":"before_bad_edit"}`},
				{Name: "apply_patch", Arguments: "*** Add File: checkpoint_result.txt\n"},
			},
		},
	}
	required := []evalharness.ToolCallRequirement{
		{ToolName: "checkpoint", ArgumentEquals: map[string]string{"action": "create", "checkpoint_id": "before_bad_edit"}},
		{ToolName: "apply_patch", ArgsContains: []string{"target.txt", "scratch.txt"}},
		{ToolName: "checkpoint", ArgumentEquals: map[string]string{"action": "restore", "checkpoint_id": "before_bad_edit"}},
		{ToolName: "apply_patch", ArgsContains: []string{"checkpoint_result.txt"}},
	}
	if got := missingRequiredToolSequence(required, messages); len(got) != 0 {
		t.Fatalf("expected no missing tool sequence, got %+v", got)
	}

	outOfOrder := []providers.ChatMessage{{
		Role: "assistant",
		ToolCalls: []providers.ToolCall{
			{Name: "checkpoint", Arguments: `{"action":"restore","checkpoint_id":"before_bad_edit"}`},
			{Name: "checkpoint", Arguments: `{"action":"create","checkpoint_id":"before_bad_edit"}`},
		},
	}}
	missing := missingRequiredToolSequence(required[:2], outOfOrder)
	if len(missing) != 1 || missing[0] != "apply_patch contains=target.txt contains=scratch.txt" {
		t.Fatalf("unexpected missing sequence: %+v", missing)
	}
}

func TestEvalSafePreviewRedactsSecretsAndTruncates(t *testing.T) {
	got := evalSafePreview("access_token=secret-value-1234567890 keep", 200)
	if strings.Contains(got, "secret-value") {
		t.Fatalf("secret leaked in preview: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("redaction marker missing: %q", got)
	}
	truncated := evalSafePreview(strings.Repeat("x", 40), 20)
	if !strings.Contains(truncated, "truncated") {
		t.Fatalf("expected truncation marker: %q", truncated)
	}
}

func TestEvalToolObservationsAreMetadataOnly(t *testing.T) {
	records := []tools.ToolExecutionRecord{{
		Name:                 "run_shell",
		CallID:               "call_1",
		ArgumentsSHA256:      strings.Repeat("c", 64),
		ResultAction:         "restore",
		Kind:                 tools.ToolKindShell,
		Exposure:             tools.ToolExposureDirect,
		Risk:                 tools.ToolRiskHigh,
		ClassificationReason: "destructive shell command",
		PolicyAction:         tools.ToolPolicyAllow,
		PolicyReason:         "risk policy",
		ReadOnly:             false,
		ConcurrencySafe:      false,
		DurationMS:           42,
		RevisionBefore:       "git:before:worktree:aaa",
		RevisionAfter:        "git:after:worktree:bbb",
		Success:              false,
		Error:                "authorization: bearer abc123",
		ErrorKind:            "approval_required",
		RawOutputBytes:       1024,
		ReturnedOutputBytes:  256,
		ResultBudgeted:       true,
		ResultRef:            "/tmp/wuu/tool-results/call_1.txt",
		ArtifactRefs:         []string{"/tmp/wuu/tool-results/call_1.txt", "/tmp/wuu/tool-results/run-test-logs/call_1.log"},
		ApprovalRef:          "/tmp/wuu/approvals/call_1.json",
		PatchRiskSummary:     &tools.ToolPatchRisk{FileCount: 2, HunkCount: 2, AddedLines: 8, DeletedLines: 3, Actions: map[string]int{"update": 2}, MultiFile: true, RiskLevel: "medium"},
	}}

	got := evalToolObservations(records)
	if len(got) != 1 {
		t.Fatalf("expected one observation, got %+v", got)
	}
	if got[0].Name != "run_shell" || got[0].Kind != "shell" || got[0].RawOutputBytes != 1024 || !got[0].ResultBudgeted {
		t.Fatalf("metadata not preserved: %+v", got[0])
	}
	if got[0].ArgumentsSHA256 != records[0].ArgumentsSHA256 {
		t.Fatalf("argument fingerprint not preserved: %+v", got[0])
	}
	if got[0].ResultAction != "restore" {
		t.Fatalf("result action not preserved: %+v", got[0])
	}
	if got[0].PolicyReason != "risk policy" {
		t.Fatalf("policy reason not preserved: %+v", got[0])
	}
	if got[0].ClassificationReason != "destructive shell command" {
		t.Fatalf("classification reason not preserved: %+v", got[0])
	}
	if got[0].RevisionBefore != records[0].RevisionBefore || got[0].RevisionAfter != records[0].RevisionAfter {
		t.Fatalf("revisions not preserved: %+v", got[0])
	}
	if strings.Contains(got[0].Error, "abc123") {
		t.Fatalf("error secret leaked: %q", got[0].Error)
	}
	if got[0].ErrorKind != "approval_required" {
		t.Fatalf("error kind not preserved: %+v", got[0])
	}
	if got[0].ResultRef != records[0].ResultRef {
		t.Fatalf("result ref not preserved: %+v", got[0])
	}
	if !reflect.DeepEqual(got[0].ArtifactRefs, records[0].ArtifactRefs) {
		t.Fatalf("artifact refs not preserved: %+v", got[0])
	}
	if got[0].ApprovalRef != records[0].ApprovalRef {
		t.Fatalf("approval ref not preserved: %+v", got[0])
	}
	if got[0].PatchRiskSummary == nil ||
		got[0].PatchRiskSummary.RiskLevel != "medium" ||
		got[0].PatchRiskSummary.Actions["update"] != 2 ||
		!got[0].PatchRiskSummary.MultiFile {
		t.Fatalf("patch risk summary not preserved: %+v", got[0].PatchRiskSummary)
	}
	if got[0].ResultEnvelope == nil || got[0].ResultEnvelope.DataRef != records[0].ResultRef {
		t.Fatalf("result envelope missing ref: %+v", got[0].ResultEnvelope)
	}
	if got[0].ResultEnvelope.Revision != records[0].RevisionAfter {
		t.Fatalf("result envelope missing revision: %+v", got[0].ResultEnvelope)
	}
	artifactRefs, ok := got[0].ResultEnvelope.Data["artifact_refs"].([]string)
	if !ok || !reflect.DeepEqual(artifactRefs, records[0].ArtifactRefs) {
		t.Fatalf("result envelope missing artifact refs: %+v", got[0].ResultEnvelope)
	}
	if got[0].ResultEnvelope.Data["approval_ref"] != records[0].ApprovalRef {
		t.Fatalf("result envelope missing approval ref: %+v", got[0].ResultEnvelope)
	}
	if got[0].ResultEnvelope.Data["error_kind"] != records[0].ErrorKind {
		t.Fatalf("result envelope missing error kind: %+v", got[0].ResultEnvelope)
	}
	if got[0].ResultEnvelope.Data["arguments_sha256"] != records[0].ArgumentsSHA256 {
		t.Fatalf("result envelope missing argument fingerprint: %+v", got[0].ResultEnvelope)
	}
	if got[0].ResultEnvelope.Data["result_action"] != records[0].ResultAction {
		t.Fatalf("result envelope missing result action: %+v", got[0].ResultEnvelope)
	}
	rawEnvelope, err := json.Marshal(got[0].ResultEnvelope)
	if err != nil {
		t.Fatalf("marshal result envelope: %v", err)
	}
	if strings.Contains(string(rawEnvelope), "abc123") || strings.Contains(string(rawEnvelope), "authorization") {
		t.Fatalf("result envelope leaked raw error: %s", string(rawEnvelope))
	}
}

func TestEvalToolInventoryObservationsAreSchemaFree(t *testing.T) {
	infos := []tools.ToolInfo{{
		Name:            "read_file",
		Kind:            tools.ToolKindFile,
		Exposure:        tools.ToolExposureDirect,
		Risk:            tools.ToolRiskLow,
		ReadOnly:        true,
		ConcurrencySafe: true,
		Reason:          "safe metadata without schema",
	}}

	got := evalToolInventoryObservations(infos)
	if len(got) != 1 {
		t.Fatalf("expected one tool inventory item, got %+v", got)
	}
	if got[0].Name != "read_file" || got[0].Kind != "file" || got[0].Exposure != "direct" || got[0].Risk != "low" || !got[0].ReadOnly || !got[0].ConcurrencySafe {
		t.Fatalf("tool inventory metadata not preserved: %+v", got[0])
	}
	raw, err := json.Marshal(got[0])
	if err != nil {
		t.Fatalf("marshal tool inventory: %v", err)
	}
	for _, forbidden := range []string{"description", "input_schema", "parameters", "properties"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("tool inventory leaked schema-like field %q: %s", forbidden, string(raw))
		}
	}
}

func TestEvalModelProfileObservation(t *testing.T) {
	got := evalModelProfileObservation(&runtime.Session{
		ProviderName: "openai",
		Model:        "gpt-5-codex",
	})

	if got == nil {
		t.Fatal("expected model profile observation")
	}
	if got.ProviderName != "openai" || got.Model != "gpt-5-codex" || got.Family != "codex" {
		t.Fatalf("unexpected model profile identity: %+v", got)
	}
	if got.DefaultWriteMode != "patch" || !got.FreeformTool || !got.AllowParallelReadOnly {
		t.Fatalf("unexpected model profile strategy: %+v", got)
	}
}

func TestEvalContextBlockObservationsSummarizeRuntimeBlocks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/eval\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nconst token = \"super-secret-token\"\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	kit, err := tools.New(root)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"main.go","offset":1,"limit":3}`,
	}); err != nil {
		t.Fatalf("read_file: %v", err)
	}

	got := evalContextBlockObservations(&runtime.Session{
		RootDir: root,
		Toolkit: kit,
	})
	byKind := make(map[string]evalharness.ContextBlockObservation)
	for _, block := range got {
		if block.Kind == "" {
			t.Fatalf("context block kind missing: %+v", block)
		}
		if block.ContentBytes <= 0 || strings.TrimSpace(block.ContentPreview) == "" {
			t.Fatalf("context block missing preview metadata: %+v", block)
		}
		if strings.Contains(block.ContentPreview, "super-secret-token") {
			t.Fatalf("context block preview leaked file body: %+v", block)
		}
		byKind[block.Kind] = block
	}
	for _, kind := range []string{"ENVIRONMENT", "REPO_MAP", "ACTIVE_FILES", "TOOL_RESULT_SUMMARY"} {
		if _, ok := byKind[kind]; !ok {
			t.Fatalf("missing context block kind %s in %+v", kind, got)
		}
	}
	active := byKind["ACTIVE_FILES"]
	if active.Source != "read_file" || active.TokenBudget == 0 || !strings.Contains(active.ContentPreview, "main.go") {
		t.Fatalf("active files block missing read metadata: %+v", active)
	}
}

func TestEvalWorkflowObservationsIncludeTeamArbitration(t *testing.T) {
	store := workflow.NewStore(t.TempDir())
	if _, err := store.CreateRun(workflow.Run{
		ID:         "team-run",
		Driver:     workflow.RunDriverAgentManaged,
		Entrypoint: workflow.RunEntrypointNaturalLanguageAgent,
		Status:     workflow.RunStateRunning,
	}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	for _, agent := range []workflow.AgentRun{
		{
			ID:            "agent-a",
			WorkflowRunID: "team-run",
			Status:        workflow.AgentRunStateCompleted,
			ReportMissing: true,
			ChangedFiles:  []string{"shared.go"},
		},
		{
			ID:            "agent-b",
			WorkflowRunID: "team-run",
			Status:        workflow.AgentRunStateFailed,
			ChangedFiles:  []string{"shared.go"},
		},
	} {
		if err := store.UpsertAgentRun(agent); err != nil {
			t.Fatalf("UpsertAgentRun(%s): %v", agent.ID, err)
		}
	}

	snapshot := goalrunner.SnapshotSystem(goalrunner.SnapshotOptions{WorkflowStore: store})
	if len(snapshot.Warnings) > 0 {
		t.Fatalf("unexpected warnings: %+v", snapshot.Warnings)
	}
	attention := evalGoalAttentionObservations(snapshot.Attention)
	if len(attention) == 0 {
		t.Fatalf("goal attention should capture workflow arbitration issues: %+v", snapshot.Attention)
	}
	got := evalWorkflowObservations(snapshot.Workflows)
	if len(got) != 1 {
		t.Fatalf("expected one workflow observation, got %+v", got)
	}
	if got[0].Driver != workflow.RunDriverAgentManaged || got[0].Entrypoint != workflow.RunEntrypointNaturalLanguageAgent {
		t.Fatalf("workflow observation missing driver fields: %+v", got[0])
	}
	wantRunDir := filepath.Join(store.Dir(), "workflows", "team-run")
	if got[0].RunDir != wantRunDir || got[0].EventLogPath != filepath.Join(wantRunDir, "events.jsonl") {
		t.Fatalf("workflow observation missing artifact paths: %+v", got[0])
	}
	arbitration := got[0].TeamArbitration
	if arbitration.Status != "attention_required" {
		t.Fatalf("unexpected arbitration status: %+v", arbitration)
	}
	if len(arbitration.MissingReports) != 1 || arbitration.MissingReports[0] != "agent-a" {
		t.Fatalf("missing reports not preserved: %+v", arbitration)
	}
	if len(arbitration.FailedAgentRuns) != 1 || arbitration.FailedAgentRuns[0] != "agent-b" {
		t.Fatalf("failed runs not preserved: %+v", arbitration)
	}
	if len(arbitration.ChangedFileOverlaps) != 1 || arbitration.ChangedFileOverlaps[0].File != "shared.go" {
		t.Fatalf("changed-file overlap not preserved: %+v", arbitration)
	}
}

func TestPersistEvalTraceWritesSessionArtifact(t *testing.T) {
	sessionDir := t.TempDir()
	result := evalharness.Result{
		TaskID:   "task-1",
		TaskName: "Task One",
		Observability: &evalharness.Observability{
			SessionDir:         sessionDir,
			FinalAnswerPreview: "done",
			ModelProfile:       &evalharness.ModelProfileObservation{ProviderName: "openai", Model: "gpt-5-codex", Family: "codex"},
		},
	}

	persistEvalTrace(&result)

	if result.Observability.TracePath == "" {
		t.Fatalf("trace path not recorded: %+v", result.Observability)
	}
	data, err := os.ReadFile(result.Observability.TracePath)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	if !strings.Contains(string(data), `"type":"model_profile"`) || !strings.Contains(string(data), `"type":"final"`) {
		t.Fatalf("trace missing expected events:\n%s", string(data))
	}
}

func TestRunEvalReplayTraceJSON(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "eval-trace.jsonl")
	if err := evalharness.WriteTrace(tracePath, evalharness.Result{
		TaskID:             "task-1",
		TaskName:           "Task One",
		Success:            true,
		VerificationReason: "passed",
		Observability: &evalharness.Observability{
			SessionID:          "eval-task-1",
			SessionDir:         filepath.Dir(tracePath),
			TracePath:          tracePath,
			FinalAnswerPreview: "done",
			ModelProfile:       &evalharness.ModelProfileObservation{ProviderName: "openai", Model: "gpt-5-codex", Family: "codex"},
			ContextBlocks: []evalharness.ContextBlockObservation{{
				Kind:           "TOOL_RESULT_SUMMARY",
				Source:         "tool_telemetry",
				TokenBudget:    800,
				ContentPreview: "recent_tool_calls:",
			}},
			ToolRecords: []evalharness.ToolObservation{{
				Name:            "read_file",
				ArgumentsSHA256: strings.Repeat("c", 64),
				Success:         true,
			}, {
				Name:            "read_file",
				ArgumentsSHA256: strings.Repeat("c", 64),
				Success:         true,
			}},
		},
	}); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	outputPath := filepath.Join(t.TempDir(), "replay.json")

	output := captureStdout(t, func() {
		if err := run([]string{"eval", "--replay-trace", tracePath, "--json", "--output", outputPath}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	var stdoutSummary evalharness.TraceReplaySummary
	if err := json.Unmarshal([]byte(output), &stdoutSummary); err != nil {
		t.Fatalf("expected replay JSON output, got %q: %v", output, err)
	}
	if stdoutSummary.Task == nil || stdoutSummary.Task.ID != "task-1" || stdoutSummary.Final == nil || !stdoutSummary.Final.Success {
		t.Fatalf("unexpected stdout replay summary: %+v", stdoutSummary)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read replay output: %v", err)
	}
	var fileSummary evalharness.TraceReplaySummary
	if err := json.Unmarshal(data, &fileSummary); err != nil {
		t.Fatalf("parse replay output file: %v", err)
	}
	if fileSummary.Mode != "deterministic_trace_replay" || len(fileSummary.ToolNames) != 2 || fileSummary.ToolNames[0] != "read_file" || fileSummary.ToolNames[1] != "read_file" {
		t.Fatalf("unexpected replay output file: %+v", fileSummary)
	}
	if len(fileSummary.ContextBlocks) != 1 ||
		fileSummary.ContextBlocks[0].Kind != "TOOL_RESULT_SUMMARY" ||
		fileSummary.ContextBlocks[0].ContentPreview != "recent_tool_calls:" {
		t.Fatalf("replay output should include context block observations: %+v", fileSummary.ContextBlocks)
	}
	if fileSummary.ToolSummary == nil ||
		len(fileSummary.ToolSummary.RepeatedArguments) != 1 ||
		fileSummary.ToolSummary.RepeatedArguments[0].ToolName != "read_file" ||
		fileSummary.ToolSummary.RepeatedArguments[0].ArgumentsSHA256 != strings.Repeat("c", 64) ||
		fileSummary.ToolSummary.RepeatedArguments[0].Count != 2 {
		t.Fatalf("replay output should include repeated argument summary: %+v", fileSummary.ToolSummary)
	}
}

func TestRunEvalReplayTraceTextPrintsPolicyBlocks(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "eval-trace.jsonl")
	approvalRef := "/tmp/wuu/session/approvals/call-process.json"
	if err := evalharness.WriteTrace(tracePath, evalharness.Result{
		TaskID:             "task-1",
		TaskName:           "Task One",
		Success:            true,
		ForbiddenToolsUsed: []string{"create_workflow"},
		WorkflowIssues:     []string{"run-1:missing_reports=beta-writer"},
		VerificationEvidence: []evalharness.VerificationEvidence{{
			Check:   "go tests",
			Passed:  true,
			Command: "go test ./...",
		}},
		Observability: &evalharness.Observability{
			ToolRecords: []evalharness.ToolObservation{{
				Name:         "run_test",
				CallID:       "call-test",
				ResultAction: "run",
				Success:      true,
			}, {
				Name:            "start_process",
				CallID:          "call-process",
				Kind:            "process",
				Risk:            "high",
				PolicyAction:    "require_approval",
				ErrorKind:       "approval_required",
				ApprovalRef:     approvalRef,
				ArgumentsSHA256: strings.Repeat("e", 64),
				Success:         false,
			}},
			GoalAttention: []evalharness.GoalAttentionObservation{{
				Source:  "workflow_agent",
				ID:      "beta-writer",
				Status:  "missing_report",
				Message: "workflow run run-1 is missing agent report",
			}},
			WorkflowRuns: []evalharness.WorkflowRunObservation{{
				ID:           "run-1",
				RunDir:       "/tmp/wuu/workflows/run-1",
				EventLogPath: "/tmp/wuu/workflows/run-1/events.jsonl",
				Driver:       "agent_managed",
				Status:       "completed",
				AgentRuns: []evalharness.WorkflowAgentRunObservation{{
					ID:           "alpha-writer",
					TaskName:     "write alpha marker",
					AgentProfile: "marker-writer",
					Status:       "completed",
					ReportPath:   "/tmp/wuu/workflows/run-1/agents/alpha/report.md",
					WorktreePath: "/tmp/wuu/worktrees/alpha",
					ChangedFiles: []string{"team_alpha.txt"},
				}, {
					ID:            "beta-writer",
					TaskName:      "write beta marker",
					Status:        "awaiting_report",
					ReportMissing: true,
					ChangedFiles:  []string{"team_alpha.txt", "team_beta.txt"},
				}},
				TeamArbitration: evalharness.WorkflowTeamArbitration{
					Status:         "awaiting_reports",
					MissingReports: []string{"beta-writer"},
					ChangedFileOverlaps: []evalharness.WorkflowChangedFileOverlapObservation{{
						File:        "team_alpha.txt",
						AgentRunIDs: []string{"alpha-writer", "beta-writer"},
					}},
					NextActions: []string{"await_reports"},
				},
			}},
		},
	}); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run([]string{"eval", "--replay-trace", tracePath}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	if !strings.Contains(output, "policy_blocks: start_process:require_approval:approval_required:call_id=call-process:approval_ref="+approvalRef) {
		t.Fatalf("replay text output missing policy blocks:\n%s", output)
	}
	if !strings.Contains(output, "workflow_runs: run-1:driver=agent_managed:status=completed:event_log=/tmp/wuu/workflows/run-1/events.jsonl:run_dir=/tmp/wuu/workflows/run-1") {
		t.Fatalf("replay text output missing workflow artifact paths:\n%s", output)
	}
	if !strings.Contains(output, "goal_attention: workflow_agent:id=beta-writer:status=missing_report:message=workflow run run-1 is missing agent report") {
		t.Fatalf("replay text output missing goal attention:\n%s", output)
	}
	if !strings.Contains(output, "workflow_agents: run-1/alpha-writer:task=write alpha marker:profile=marker-writer:status=completed:report=/tmp/wuu/workflows/run-1/agents/alpha/report.md:worktree=/tmp/wuu/worktrees/alpha:changed=team_alpha.txt") {
		t.Fatalf("replay text output missing workflow agent report paths:\n%s", output)
	}
	if !strings.Contains(output, "run-1/beta-writer:task=write beta marker:status=awaiting_report:report_missing=true:changed=team_alpha.txt|team_beta.txt") {
		t.Fatalf("replay text output missing workflow agent missing-report state:\n%s", output)
	}
	if !strings.Contains(output, "workflow_arbitration: run-1:status=awaiting_reports:missing_reports=beta-writer:overlaps=team_alpha.txt=alpha-writer+beta-writer:next=await_reports") {
		t.Fatalf("replay text output missing workflow arbitration summary:\n%s", output)
	}
	if !strings.Contains(output, "forbidden_tools: create_workflow") {
		t.Fatalf("replay text output missing forbidden tools:\n%s", output)
	}
	if !strings.Contains(output, "workflow_issues: run-1:missing_reports=beta-writer") {
		t.Fatalf("replay text output missing workflow issues:\n%s", output)
	}
	if !strings.Contains(output, "validation: status=incomplete tools=1 evidence=1 missing=2 failures=0") {
		t.Fatalf("replay text output missing validation summary:\n%s", output)
	}
	if !strings.Contains(output, "validation_evidence: go tests:passed:command=go test ./...") {
		t.Fatalf("replay text output missing validation evidence:\n%s", output)
	}
	if !strings.Contains(output, "validation_missing: forbidden_tool:create_workflow") {
		t.Fatalf("replay text output missing validation missing requirements:\n%s", output)
	}
	if !strings.Contains(output, "workflow_issue:run-1:missing_reports=beta-writer") {
		t.Fatalf("replay text output missing workflow validation issue:\n%s", output)
	}
	if strings.Contains(output, strings.Repeat("e", 64)) {
		t.Fatalf("replay text output should not print argument fingerprints by default:\n%s", output)
	}
}

func TestApplyEvalWorkflowIssuesFailsResult(t *testing.T) {
	result := evalharness.Result{
		TaskID:  "task-1",
		Success: true,
		Observability: &evalharness.Observability{
			WorkflowRuns: []evalharness.WorkflowRunObservation{{
				ID:     "run-1",
				Status: "completed",
				TeamArbitration: evalharness.WorkflowTeamArbitration{
					Status:         "attention_required",
					MissingReports: []string{"worker-1"},
				},
			}},
		},
	}

	applyEvalWorkflowIssues(&result)

	if result.Success {
		t.Fatalf("workflow issues should fail eval result: %+v", result)
	}
	if len(result.WorkflowIssues) != 2 ||
		result.WorkflowIssues[0] != "run-1:arbitration=attention_required" ||
		result.WorkflowIssues[1] != "run-1:missing_reports=worker-1" {
		t.Fatalf("workflow issues not summarized: %+v", result.WorkflowIssues)
	}
}

func TestApplyEvalWorkflowIssuesPrefersGoalAttention(t *testing.T) {
	result := evalharness.Result{
		TaskID:  "task-1",
		Success: true,
		Observability: &evalharness.Observability{
			GoalAttention: []evalharness.GoalAttentionObservation{{
				Source:  "workflow_agent",
				ID:      "worker-1",
				Status:  "missing_report",
				Message: "workflow run run-1 is missing agent report",
			}, {
				Source: "harness",
				ID:     "task-1",
				Status: "failed",
			}},
			WorkflowRuns: []evalharness.WorkflowRunObservation{{
				ID:     "run-1",
				Status: "completed",
			}},
		},
	}

	applyEvalWorkflowIssues(&result)

	if result.Success {
		t.Fatalf("goal attention issues should fail eval result: %+v", result)
	}
	want := []string{"harness:task-1:status=failed", "run-1:missing_reports=worker-1"}
	if strings.Join(result.WorkflowIssues, "\n") != strings.Join(want, "\n") {
		t.Fatalf("goal attention issues not summarized: %+v", result.WorkflowIssues)
	}
}

func TestRunEvalReplaySessionTraceJSON(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "session-trace.jsonl")
	if err := sessiontrace.AppendTurn(tracePath,
		sessiontrace.TurnRecord{
			ThreadID:     "thread-1",
			TurnID:       "turn-1",
			Status:       "completed",
			ProviderName: "openai",
			Model:        "gpt-test",
		},
		sessiontrace.FinalRecord{Status: "completed", FinalAnswerPreview: "done"},
		[]tools.ToolInfo{{Name: "semantic_search", Kind: tools.ToolKindSearch, Risk: tools.ToolRiskLow, ReadOnly: true}},
		[]tools.ToolExecutionRecord{{
			Name:            "semantic_search",
			ArgumentsSHA256: strings.Repeat("d", 64),
			Kind:            tools.ToolKindSearch,
			Success:         true,
		}, {
			Name:            "semantic_search",
			ArgumentsSHA256: strings.Repeat("d", 64),
			Kind:            tools.ToolKindSearch,
			Success:         true,
		}},
	); err != nil {
		t.Fatalf("write session trace: %v", err)
	}
	outputPath := filepath.Join(t.TempDir(), "session-replay.json")

	output := captureStdout(t, func() {
		if err := run([]string{"eval", "--replay-trace", tracePath, "--json", "--output", outputPath}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	var stdoutSummary sessiontrace.ReplaySummary
	if err := json.Unmarshal([]byte(output), &stdoutSummary); err != nil {
		t.Fatalf("expected session replay JSON output, got %q: %v", output, err)
	}
	if stdoutSummary.Mode != "session_trace_replay" || stdoutSummary.LatestTurn == nil || stdoutSummary.LatestTurn.ThreadID != "thread-1" {
		t.Fatalf("unexpected stdout session replay summary: %+v", stdoutSummary)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read session replay output: %v", err)
	}
	var fileSummary sessiontrace.ReplaySummary
	if err := json.Unmarshal(data, &fileSummary); err != nil {
		t.Fatalf("parse session replay output file: %v", err)
	}
	if len(fileSummary.ToolNames) != 2 || fileSummary.ToolNames[0] != "semantic_search" || fileSummary.ToolNames[1] != "semantic_search" || fileSummary.Final == nil || fileSummary.Final.Status != "completed" {
		t.Fatalf("unexpected session replay output file: %+v", fileSummary)
	}
	if fileSummary.ToolSummary == nil ||
		len(fileSummary.ToolSummary.RepeatedArguments) != 1 ||
		fileSummary.ToolSummary.RepeatedArguments[0].ToolName != "semantic_search" ||
		fileSummary.ToolSummary.RepeatedArguments[0].ArgumentsSHA256 != strings.Repeat("d", 64) ||
		fileSummary.ToolSummary.RepeatedArguments[0].Count != 2 {
		t.Fatalf("session replay output should include repeated argument summary: %+v", fileSummary.ToolSummary)
	}
}

func TestRunEvalReplaySessionTraceTextPrintsPolicyBlocks(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "session-trace.jsonl")
	if err := sessiontrace.AppendTurn(tracePath,
		sessiontrace.TurnRecord{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			Status:   "completed",
		},
		sessiontrace.FinalRecord{Status: "completed", FinalAnswerPreview: "done"},
		nil,
		[]tools.ToolExecutionRecord{{
			Name:            "run_shell",
			CallID:          "call-shell",
			Kind:            tools.ToolKindShell,
			Risk:            tools.ToolRiskHigh,
			PolicyAction:    tools.ToolPolicyDeny,
			ErrorKind:       "policy_denied",
			ArgumentsSHA256: strings.Repeat("f", 64),
			Success:         false,
		}},
	); err != nil {
		t.Fatalf("write session trace: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run([]string{"eval", "--replay-trace", tracePath}); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	})

	if !strings.Contains(output, "policy_blocks: run_shell:deny:policy_denied:call_id=call-shell") {
		t.Fatalf("session replay text output missing policy blocks:\n%s", output)
	}
	if strings.Contains(output, strings.Repeat("f", 64)) {
		t.Fatalf("session replay text output should not print argument fingerprints by default:\n%s", output)
	}
}

func TestSetTemporaryEnvRestoresPreviousValue(t *testing.T) {
	t.Setenv("WUU_HOME", "/tmp/original-wuu-home")
	restore := setTemporaryEnv("WUU_HOME", "/tmp/eval-wuu-home")
	if got := os.Getenv("WUU_HOME"); got != "/tmp/eval-wuu-home" {
		t.Fatalf("WUU_HOME = %q, want temporary value", got)
	}
	restore()
	if got := os.Getenv("WUU_HOME"); got != "/tmp/original-wuu-home" {
		t.Fatalf("WUU_HOME = %q, want original value", got)
	}
}

func TestSetTemporaryEnvUnsetsMissingValue(t *testing.T) {
	t.Setenv("WUU_HOME", "placeholder")
	os.Unsetenv("WUU_HOME")
	restore := setTemporaryEnv("WUU_HOME", "/tmp/eval-wuu-home")
	if got := os.Getenv("WUU_HOME"); got != "/tmp/eval-wuu-home" {
		t.Fatalf("WUU_HOME = %q, want temporary value", got)
	}
	restore()
	if _, ok := os.LookupEnv("WUU_HOME"); ok {
		t.Fatal("WUU_HOME should be unset after restore")
	}
}

func TestResolveContextWindow_PrefersProviderOverride(t *testing.T) {
	provider := config.ProviderConfig{
		ContextWindow: 777,
		Models: map[string]config.ProviderModelConfig{
			"gpt-5.4": {
				Limit: &config.ProviderModelLimitConfig{Context: 1_050_000},
			},
		},
	}
	if got := runtime.ResolveContextWindow("gpt-5.4", provider, 555); got != 777 {
		t.Fatalf("expected provider override, got %d", got)
	}
}

func TestResolveContextWindow_FallsBackToAgentOverride(t *testing.T) {
	if got := runtime.ResolveContextWindow("gpt-5.4", config.ProviderConfig{}, 555); got != 555 {
		t.Fatalf("expected agent override, got %d", got)
	}
}

func TestResolveContextWindow_UsesModelRegistryByDefault(t *testing.T) {
	if got := runtime.ResolveContextWindow("gpt-5.4", config.ProviderConfig{}, 0); got != 400000 {
		t.Fatalf("expected model registry context window, got %d", got)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	defer r.Close()

	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}

	return strings.TrimSpace(buf.String())
}

func withStdin[T any](t *testing.T, text string, fn func() T) T {
	t.Helper()

	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	if _, err := io.WriteString(w, text); err != nil {
		t.Fatalf("write stdin pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()
	return fn()
}

type cliExecFakeController struct {
	initResult appserver.InitializeResult
	thread     appserver.Thread
	turn       appserver.Turn
	events     []wuuexec.Notification

	startedThread  bool
	startEphemeral bool
	resumedThread  string
	forkedThread   string
	startedPrompt  string
	startedImages  []appserver.TurnStartImage
	startedFiles   []appserver.TurnStartFile
}

func newCLIExecFakeController(events ...wuuexec.Notification) *cliExecFakeController {
	return &cliExecFakeController{
		initResult: appserver.InitializeResult{
			ProtocolVersion: appserver.ProtocolVersion,
			Provider:        "test-provider",
			Model:           "test-model",
			WorkspaceRoot:   "/repo",
			Permissions:     appserver.PermissionSummary{Mode: "default"},
		},
		thread: appserver.Thread{ID: "thread-1", ModelProvider: "test-provider", Model: "test-model", CWD: "/repo"},
		turn:   appserver.Turn{ID: "turn-1"},
		events: events,
	}
}

func (f *cliExecFakeController) Initialize(context.Context) (appserver.InitializeResult, error) {
	return f.initResult, nil
}

func (f *cliExecFakeController) StartThread(_ context.Context, ephemeral bool) (appserver.Thread, error) {
	f.startedThread = true
	f.startEphemeral = ephemeral
	f.thread.Ephemeral = ephemeral
	return f.thread, nil
}

func (f *cliExecFakeController) ResumeThread(_ context.Context, id string) (appserver.Thread, error) {
	f.resumedThread = id
	return f.thread, nil
}

func (f *cliExecFakeController) ForkThread(_ context.Context, id string) (appserver.Thread, error) {
	f.forkedThread = id
	f.thread.ID = "fork-thread-1"
	f.thread.ForkedFromID = id
	return f.thread, nil
}

func (f *cliExecFakeController) StartTurn(_ context.Context, _ string, input wuuexec.TurnInput) (appserver.Turn, error) {
	f.startedPrompt = input.Prompt
	f.startedImages = append([]appserver.TurnStartImage(nil), input.Images...)
	f.startedFiles = append([]appserver.TurnStartFile(nil), input.Files...)
	return f.turn, nil
}

func (f *cliExecFakeController) Interrupt(context.Context, string) error {
	return nil
}

func (f *cliExecFakeController) Shutdown(context.Context) error {
	return nil
}

func (f *cliExecFakeController) Notifications() <-chan wuuexec.Notification {
	ch := make(chan wuuexec.Notification, len(f.events))
	for _, event := range f.events {
		ch <- event
	}
	return ch
}

func installExecControllerOverride(t *testing.T, controller wuuexec.Controller) func() {
	t.Helper()
	previous := execControllerOverride
	execControllerOverride = controller
	return func() {
		execControllerOverride = previous
	}
}

type fakeDebugAppServerClient struct {
	opts     debugAppServerOptions
	calls    []fakeDebugCall
	results  map[string]json.RawMessage
	shutdown bool
}

type fakeDebugCall struct {
	method string
	params json.RawMessage
}

func (f *fakeDebugAppServerClient) Call(_ context.Context, method string, params any, result any) error {
	var rawParams json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		rawParams = append(json.RawMessage(nil), data...)
	}
	f.calls = append(f.calls, fakeDebugCall{method: method, params: rawParams})

	data := f.results[method]
	if len(data) == 0 {
		data = json.RawMessage(`null`)
	}
	if result == nil {
		return nil
	}
	if rawResult, ok := result.(*json.RawMessage); ok {
		*rawResult = append(json.RawMessage(nil), data...)
		return nil
	}
	return json.Unmarshal(data, result)
}

func (f *fakeDebugAppServerClient) Shutdown(context.Context) error {
	f.shutdown = true
	return nil
}

func installDebugAppServerClientOverride(t *testing.T, client *fakeDebugAppServerClient) func() {
	t.Helper()
	previous := debugAppServerClientOverride
	debugAppServerClientOverride = func(_ context.Context, opts debugAppServerOptions) (debugAppServerClient, error) {
		client.opts = opts
		return client, nil
	}
	return func() {
		debugAppServerClientOverride = previous
	}
}

func cliExecNotification(method string, params any) wuuexec.Notification {
	data, err := json.Marshal(params)
	if err != nil {
		panic(err)
	}
	return wuuexec.Notification{Method: method, Params: data}
}

func parseCLIJSONLines(t *testing.T, text string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(text), "\n")
	events := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid JSONL line %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestRunSessionShowReturnsCreatedSessionJSON(t *testing.T) {
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	t.Setenv("WUU_HOME", wuuHome)

	home, err := statepath.Home("")
	if err != nil {
		t.Fatalf("statepath.Home: %v", err)
	}
	sessDir := statepath.SessionsDir(home)
	if sessDir == "" {
		t.Fatal("statepath.SessionsDir returned empty path")
	}

	const id = "cli-thread-1"
	sess, err := session.CreateWithMetadata(sessDir, id, "/tmp/workdir")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := session.AppendHistoryRecord(sessDir, sess.ID, session.HistoryRecord{
		Role:    "user",
		Content: "hello from CLI",
	}); err != nil {
		t.Fatalf("append history: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run([]string{"session-show", "--thread", sess.ID, "--json"}); err != nil {
			t.Fatalf("run session-show: %v", err)
		}
	})

	var payload struct {
		ThreadID string                  `json:"thread_id"`
		Session  session.Session         `json:"session"`
		History  []session.HistoryRecord `json:"history"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("parse JSON: %v\noutput: %s", err, output)
	}
	if payload.ThreadID != sess.ID {
		t.Errorf("expected thread_id %q, got %q", sess.ID, payload.ThreadID)
	}
	if len(payload.History) != 1 || payload.History[0].Content != "hello from CLI" {
		t.Errorf("unexpected history: %+v", payload.History)
	}
}

func TestRunSessionListJSONFiltersCurrentWorkspace(t *testing.T) {
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	t.Setenv("WUU_HOME", wuuHome)
	workdir := t.TempDir()
	otherWorkdir := t.TempDir()
	t.Chdir(workdir)

	home, err := statepath.Home("")
	if err != nil {
		t.Fatalf("statepath.Home: %v", err)
	}
	sessDir := statepath.SessionsDir(home)
	if _, err := session.CreateWithMetadata(sessDir, "current-thread", workdir); err != nil {
		t.Fatalf("create current session: %v", err)
	}
	if _, err := session.CreateWithMetadata(sessDir, "other-thread", otherWorkdir); err != nil {
		t.Fatalf("create other session: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run([]string{"session", "list", "--json"}); err != nil {
			t.Fatalf("run session list: %v", err)
		}
	})

	var payload struct {
		Sessions []session.Session `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("parse JSON: %v\noutput: %s", err, output)
	}
	if len(payload.Sessions) != 1 || payload.Sessions[0].ID != "current-thread" {
		t.Fatalf("unexpected sessions: %+v", payload.Sessions)
	}
}

func TestRunSessionShowSubcommandReturnsHistoryJSON(t *testing.T) {
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	t.Setenv("WUU_HOME", wuuHome)
	workdir := t.TempDir()

	home, err := statepath.Home("")
	if err != nil {
		t.Fatalf("statepath.Home: %v", err)
	}
	sessDir := statepath.SessionsDir(home)
	sess, err := session.CreateWithMetadata(sessDir, "show-thread", workdir)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := session.AppendHistoryRecord(sessDir, sess.ID, session.HistoryRecord{
		Role:    "assistant",
		Content: "visible answer",
	}); err != nil {
		t.Fatalf("append history: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run([]string{"session", "show", "--json", sess.ID}); err != nil {
			t.Fatalf("run session show: %v", err)
		}
	})

	var payload struct {
		ThreadID string                  `json:"thread_id"`
		History  []session.HistoryRecord `json:"history"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("parse JSON: %v\noutput: %s", err, output)
	}
	if payload.ThreadID != sess.ID || len(payload.History) != 1 || payload.History[0].Content != "visible answer" {
		t.Fatalf("unexpected session show payload: %+v", payload)
	}
}

func TestRunSessionSearchJSONMatchesHistoryAndFiltersWorkspace(t *testing.T) {
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	t.Setenv("WUU_HOME", wuuHome)
	workdir := t.TempDir()
	otherWorkdir := t.TempDir()
	t.Chdir(workdir)

	home, err := statepath.Home("")
	if err != nil {
		t.Fatalf("statepath.Home: %v", err)
	}
	sessDir := statepath.SessionsDir(home)
	current, err := session.CreateWithMetadata(sessDir, "search-current", workdir)
	if err != nil {
		t.Fatalf("create current session: %v", err)
	}
	if err := session.AppendHistoryRecord(sessDir, current.ID, session.HistoryRecord{
		Role:    "user",
		Content: "please fix the orion cache regression",
	}); err != nil {
		t.Fatalf("append current history: %v", err)
	}
	other, err := session.CreateWithMetadata(sessDir, "search-other", otherWorkdir)
	if err != nil {
		t.Fatalf("create other session: %v", err)
	}
	if err := session.AppendHistoryRecord(sessDir, other.ID, session.HistoryRecord{
		Role:    "user",
		Content: "orion cache but another workspace",
	}); err != nil {
		t.Fatalf("append other history: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run([]string{"session", "search", "--json", "orion cache"}); err != nil {
			t.Fatalf("run session search: %v", err)
		}
	})

	var payload struct {
		Query   string `json:"query"`
		Results []struct {
			ThreadID string          `json:"thread_id"`
			Session  session.Session `json:"session"`
			Snippet  string          `json:"snippet"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("parse JSON: %v\noutput: %s", err, output)
	}
	if payload.Query != "orion cache" {
		t.Fatalf("query = %q", payload.Query)
	}
	if len(payload.Results) != 1 || payload.Results[0].ThreadID != current.ID {
		t.Fatalf("unexpected search results: %+v", payload.Results)
	}
	if !strings.Contains(payload.Results[0].Snippet, "orion cache") {
		t.Fatalf("snippet should contain match context: %+v", payload.Results[0])
	}
}

func TestRunSessionArchiveHidesSessionFromList(t *testing.T) {
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	t.Setenv("WUU_HOME", wuuHome)
	workdir := t.TempDir()
	t.Chdir(workdir)

	home, err := statepath.Home("")
	if err != nil {
		t.Fatalf("statepath.Home: %v", err)
	}
	sessDir := statepath.SessionsDir(home)
	sess, err := session.CreateWithMetadata(sessDir, "archive-thread", workdir)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	archiveOutput := captureStdout(t, func() {
		if err := run([]string{"session", "archive", "--json", sess.ID}); err != nil {
			t.Fatalf("run session archive: %v", err)
		}
	})
	var archivePayload struct {
		ThreadID string          `json:"thread_id"`
		Session  session.Session `json:"session"`
		Archived bool            `json:"archived"`
	}
	if err := json.Unmarshal([]byte(archiveOutput), &archivePayload); err != nil {
		t.Fatalf("parse archive JSON: %v\noutput: %s", err, archiveOutput)
	}
	if archivePayload.ThreadID != sess.ID || !archivePayload.Archived || archivePayload.Session.ArchivedAt == nil {
		t.Fatalf("unexpected archive payload: %+v", archivePayload)
	}

	listOutput := captureStdout(t, func() {
		if err := run([]string{"session", "list", "--json"}); err != nil {
			t.Fatalf("run session list: %v", err)
		}
	})
	var listPayload struct {
		Sessions []session.Session `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(listOutput), &listPayload); err != nil {
		t.Fatalf("parse list JSON: %v\noutput: %s", err, listOutput)
	}
	if len(listPayload.Sessions) != 0 {
		t.Fatalf("archived session should be hidden from default list: %+v", listPayload.Sessions)
	}

	includeArchivedOutput := captureStdout(t, func() {
		if err := run([]string{"session", "list", "--json", "--include-archived"}); err != nil {
			t.Fatalf("run session list include archived: %v", err)
		}
	})
	if err := json.Unmarshal([]byte(includeArchivedOutput), &listPayload); err != nil {
		t.Fatalf("parse include archived JSON: %v\noutput: %s", err, includeArchivedOutput)
	}
	if len(listPayload.Sessions) != 1 || listPayload.Sessions[0].ID != sess.ID {
		t.Fatalf("include archived should return archived session: %+v", listPayload.Sessions)
	}
}

func TestRunSessionDeleteRemovesSessionAndArtifacts(t *testing.T) {
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	t.Setenv("WUU_HOME", wuuHome)
	workdir := t.TempDir()
	t.Chdir(workdir)

	home, err := statepath.Home("")
	if err != nil {
		t.Fatalf("statepath.Home: %v", err)
	}
	sessDir := statepath.SessionsDir(home)
	sess, err := session.CreateWithMetadata(sessDir, "delete-thread", workdir)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := session.AppendHistoryRecord(sessDir, sess.ID, session.HistoryRecord{Role: "user", Content: "temporary task"}); err != nil {
		t.Fatalf("append history: %v", err)
	}
	workspaceStateDir, err := statepath.WorkspaceDir(wuuHome, workdir)
	if err != nil {
		t.Fatalf("WorkspaceDir: %v", err)
	}
	artifactDir := statepath.SessionArtifactDir(workspaceStateDir, sess.ID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("create artifact dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "trace.jsonl"), []byte(`{"type":"turn"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run([]string{"session", "delete", "--json", sess.ID}); err != nil {
			t.Fatalf("run session delete: %v", err)
		}
	})

	var payload struct {
		ThreadID         string          `json:"thread_id"`
		Session          session.Session `json:"session"`
		Deleted          bool            `json:"deleted"`
		ArtifactPath     string          `json:"artifact_path"`
		ArtifactsDeleted bool            `json:"artifacts_deleted"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("parse delete JSON: %v\noutput: %s", err, output)
	}
	if payload.ThreadID != sess.ID || payload.Session.ID != sess.ID || !payload.Deleted || payload.ArtifactPath != artifactDir || !payload.ArtifactsDeleted {
		t.Fatalf("unexpected delete payload: %+v", payload)
	}
	if _, ok, err := session.Find(sessDir, sess.ID); err != nil || ok {
		t.Fatalf("deleted session should not be found, ok=%v err=%v", ok, err)
	}
	if _, err := os.Stat(artifactDir); !os.IsNotExist(err) {
		t.Fatalf("artifact dir should be removed, err=%v", err)
	}
}

func TestRunDebugAppServerInitializeUsesClient(t *testing.T) {
	client := &fakeDebugAppServerClient{
		results: map[string]json.RawMessage{
			appserver.MethodInitialize: json.RawMessage(`{"protocol_version":"test/v1","provider":"p","model":"m"}`),
		},
	}
	restore := installDebugAppServerClientOverride(t, client)
	defer restore()

	output := captureStdout(t, func() {
		if err := run([]string{"debug", "app-server", "initialize", "--workdir", "/tmp/repo", "--provider", "p", "--model", "m", "--no-tools"}); err != nil {
			t.Fatalf("run debug app-server initialize: %v", err)
		}
	})

	if len(client.calls) != 1 || client.calls[0].method != appserver.MethodInitialize {
		t.Fatalf("unexpected calls: %+v", client.calls)
	}
	if client.opts.workdir != "/tmp/repo" || client.opts.provider != "p" || client.opts.model != "m" || !client.opts.noTools {
		t.Fatalf("options not passed to debug client: %+v", client.opts)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, output)
	}
	if payload["protocol_version"] != "test/v1" || payload["provider"] != "p" {
		t.Fatalf("unexpected initialize output: %+v", payload)
	}
	if !client.shutdown {
		t.Fatal("debug client should be shut down")
	}
}

func TestRunDebugAppServerSendForwardsMethodAndParams(t *testing.T) {
	client := &fakeDebugAppServerClient{
		results: map[string]json.RawMessage{
			appserver.MethodThreadResume: json.RawMessage(`{"thread":{"id":"thread-1"}}`),
		},
	}
	restore := installDebugAppServerClientOverride(t, client)
	defer restore()

	output := captureStdout(t, func() {
		if err := run([]string{"debug", "app-server", "send", appserver.MethodThreadResume, `{"session_id":"thread-1"}`}); err != nil {
			t.Fatalf("run debug app-server send: %v", err)
		}
	})

	if len(client.calls) != 1 || client.calls[0].method != appserver.MethodThreadResume {
		t.Fatalf("unexpected calls: %+v", client.calls)
	}
	var params map[string]any
	if err := json.Unmarshal(client.calls[0].params, &params); err != nil {
		t.Fatalf("parse params: %v", err)
	}
	if params["session_id"] != "thread-1" {
		t.Fatalf("unexpected params: %+v", params)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("parse output: %v\n%s", err, output)
	}
	thread := payload["thread"].(map[string]any)
	if thread["id"] != "thread-1" {
		t.Fatalf("unexpected output: %+v", payload)
	}
}

func TestRunDebugProtocolEventsJSONReadsTraceEvents(t *testing.T) {
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	t.Setenv("WUU_HOME", wuuHome)
	workdir := t.TempDir()

	home, err := statepath.Home("")
	if err != nil {
		t.Fatalf("statepath.Home: %v", err)
	}
	sessDir := statepath.SessionsDir(home)
	sess, err := session.CreateWithMetadata(sessDir, "debug-thread", workdir)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	workspaceStateDir, err := statepath.WorkspaceDir(wuuHome, workdir)
	if err != nil {
		t.Fatalf("WorkspaceDir: %v", err)
	}
	tracePath := sessiontrace.Path(statepath.SessionArtifactDir(workspaceStateDir, sess.ID))
	if err := sessiontrace.AppendTurn(
		tracePath,
		sessiontrace.TurnRecord{ThreadID: sess.ID, TurnID: "turn-1", Status: "completed"},
		sessiontrace.FinalRecord{Status: "completed", FinalAnswerPreview: "done"},
		nil,
		nil,
	); err != nil {
		t.Fatalf("append trace: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run([]string{"debug", "protocol", "events", "--json", sess.ID}); err != nil {
			t.Fatalf("run debug protocol events: %v", err)
		}
	})

	var payload struct {
		ThreadID  string            `json:"thread_id"`
		TracePath string            `json:"trace_path"`
		Events    []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, output)
	}
	if payload.ThreadID != sess.ID || payload.TracePath != tracePath || len(payload.Events) != 2 {
		t.Fatalf("unexpected protocol events payload: %+v", payload)
	}
	var first struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(payload.Events[0], &first); err != nil {
		t.Fatalf("parse first event: %v", err)
	}
	if first.Type != "turn" {
		t.Fatalf("first event type = %q", first.Type)
	}
}

func TestRunSessionTraceJSONReplaysTrace(t *testing.T) {
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	t.Setenv("WUU_HOME", wuuHome)
	workdir := t.TempDir()

	home, err := statepath.Home("")
	if err != nil {
		t.Fatalf("statepath.Home: %v", err)
	}
	sessDir := statepath.SessionsDir(home)
	sess, err := session.CreateWithMetadata(sessDir, "trace-thread", workdir)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	workspaceStateDir, err := statepath.WorkspaceDir(wuuHome, workdir)
	if err != nil {
		t.Fatalf("WorkspaceDir: %v", err)
	}
	tracePath := sessiontrace.Path(statepath.SessionArtifactDir(workspaceStateDir, sess.ID))
	if err := sessiontrace.AppendTurn(
		tracePath,
		sessiontrace.TurnRecord{ThreadID: sess.ID, TurnID: "turn-1", Status: "completed", InputTokens: 3, OutputTokens: 4},
		sessiontrace.FinalRecord{Status: "completed", FinalAnswerPreview: "done"},
		nil,
		nil,
	); err != nil {
		t.Fatalf("append trace: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run([]string{"session", "trace", "--json", sess.ID}); err != nil {
			t.Fatalf("run session trace: %v", err)
		}
	})

	var payload struct {
		ThreadID  string                     `json:"thread_id"`
		TracePath string                     `json:"trace_path"`
		Summary   sessiontrace.ReplaySummary `json:"summary"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("parse JSON: %v\noutput: %s", err, output)
	}
	if payload.ThreadID != sess.ID || payload.TracePath != tracePath || !payload.Summary.Complete || payload.Summary.LatestTurn == nil || payload.Summary.LatestTurn.OutputTokens != 4 {
		t.Fatalf("unexpected trace payload: %+v", payload)
	}
}

func TestRunSessionShowNotFoundReturnsError(t *testing.T) {
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	t.Setenv("WUU_HOME", wuuHome)

	err := run([]string{"session-show", "--thread", "missing-id"})
	if err == nil {
		t.Fatal("expected error for missing thread id")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Errorf("expected session-not-found, got: %v", err)
	}
}
