package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/appserver"
	"github.com/blueberrycongee/wuu/internal/config"
	wuuexec "github.com/blueberrycongee/wuu/internal/exec"
	"github.com/blueberrycongee/wuu/internal/providers/codex"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/sessiontrace"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(wuuexec.ExitCode(err))
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "init":
		return runInit(args[1:])
	case "models":
		return runModels(args[1:])
	case "run":
		return runLegacyRun(args[1:])
	case "exec":
		return runExec(args[1:])
	case "probe-title":
		return runProbeTitle(args[1:])
	case "eval":
		return runEval(args[1:])
	case "goal":
		return runGoal(args[1:])
	case "tui":
		return errors.New("the TUI has been removed; use the desktop GUI or `wuu exec` for agent-friendly text tasks")
	case "session":
		return runSession(args[1:])
	case "session-show":
		return runSessionShow(args[1:])
	case "debug":
		return runDebug(args[1:])
	case "app-server":
		return runAppServer(args[1:])
	case "version", "-v", "--version":
		if args[0] == "version" {
			return runVersion(args[1:])
		}
		return runVersion(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	long := fs.Bool("long", false, "show detailed version info")
	jsonOutput := fs.Bool("json", false, "output version as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	info := version.Info()
	if *jsonOutput {
		data, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal version info: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}
	if *long {
		fmt.Println(info.LongString())
		return nil
	}

	fmt.Println(info.String())
	return nil
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "overwrite existing .wuu.json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	workdir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current directory: %w", err)
	}
	configPath := filepath.Join(workdir, ".wuu.json")

	if !*force {
		if _, err := os.Stat(configPath); err == nil {
			return fmt.Errorf("%s already exists (use --force to overwrite)", configPath)
		}
	}

	cfg := config.Default()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	fmt.Printf("created %s\n", configPath)
	return nil
}

func runProbeTitle(args []string) error {
	fs := flag.NewFlagSet("probe-title", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	workdir := fs.String("workdir", "", "workspace directory (default: cwd)")
	threadID := fs.String("thread", "", "thread id to regenerate title for (default: most recent)")
	userPrompt := fs.String("user-prompt", "", "synthetic first user message; probe runs in dry-run mode")
	providerName := fs.String("provider", "", "override provider name from config")
	modelOverride := fs.String("model", "", "override model from config")
	dryRun := fs.Bool("dry-run", false, "do not persist the title")
	verbose := fs.Bool("verbose", true, "print every step in human-readable mode")
	jsonOut := fs.Bool("json", false, "emit TitleGenerationResult as JSON")
	quiet := fs.Bool("quiet", false, "suppress human-readable summary; implies --json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *quiet {
		*jsonOut = true
	}

	if *userPrompt == "" && *threadID == "" && *dryRun == false {
		// Default to dry-run for the implicit "use most recent thread" path
		// so an accidental invocation cannot overwrite a real title without
		// intent. Pass --dry-run=false to persist.
		*dryRun = true
	}

	rootDir, err := resolveWorkdir(*workdir)
	if err != nil {
		return err
	}
	homeDir := os.Getenv("HOME")

	opts := appserver.ProbeTitleOptions{
		WorkDir:       rootDir,
		HomeDir:       homeDir,
		ProviderName:  *providerName,
		ModelOverride: *modelOverride,
		ThreadID:      *threadID,
		UserPrompt:    *userPrompt,
		DryRun:        *dryRun,
		Verbose:       *verbose,
		JSON:          *jsonOut,
	}
	_, err = appserver.ProbeTitle(context.Background(), opts)
	if err != nil {
		// TitleGenerationResult is already pretty-printed or JSON-encoded by
		// ProbeTitle itself. We only need to surface the error to the shell.
		return err
	}
	return nil
}

func runModels(args []string) error {
	fs := flag.NewFlagSet("models", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	providerName := fs.String("provider", "", "provider name in config")
	workdir := fs.String("workdir", "", "workspace directory")
	jsonOutput := fs.Bool("json", false, "output model metadata as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rootDir, err := resolveWorkdir(*workdir)
	if err != nil {
		return err
	}
	cfg, configPath, err := config.LoadFrom(rootDir, os.Getenv("HOME"))
	if err != nil {
		return err
	}
	providerCfg, resolvedName, err := cfg.ResolveProvider(*providerName)
	if err != nil {
		return err
	}
	if !isCodexModelsProvider(providerCfg.Type) {
		return fmt.Errorf("provider %q uses type %q; live model lookup currently supports openai-codex providers only", resolvedName, providerCfg.Type)
	}

	client, err := codex.New(codex.ClientConfig{
		BaseURL: providerCfg.BaseURL,
		APIKey:  explicitProviderAPIKey(providerCfg),
		Headers: providerCfg.Headers,
	})
	if err != nil {
		return err
	}
	models, err := client.Models(context.Background())
	if err != nil {
		return err
	}
	if *jsonOutput {
		data, err := json.MarshalIndent(models, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal models: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("provider: %s\nconfig: %s\n\n", resolvedName, configPath)
	for _, model := range models {
		fmt.Println(model.Slug)
	}
	return nil
}

func runSessionShow(args []string) error {
	fs := flag.NewFlagSet("session-show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	threadID := fs.String("thread", "", "thread (session) ID; defaults to the most recent session")
	jsonOutput := fs.Bool("json", false, "output as JSON (default: human-readable)")
	limit := fs.Int("limit", 200, "max history records to print (ignored with --json)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	home, err := statepath.Home("")
	if err != nil {
		return fmt.Errorf("resolve wuu home: %w", err)
	}
	sessDir := statepath.SessionsDir(home)
	if sessDir == "" {
		return errors.New("sessions dir is empty")
	}

	id := strings.TrimSpace(*threadID)
	if id == "" {
		recent, err := session.MostRecent(sessDir)
		if err != nil {
			return fmt.Errorf("find most recent session: %w", err)
		}
		if recent == "" {
			return errors.New("no sessions found")
		}
		id = recent
	}

	meta, ok, err := session.Find(sessDir, id)
	if err != nil {
		return fmt.Errorf("lookup %q: %w", id, err)
	}
	if !ok {
		return fmt.Errorf("%w: %q", session.ErrSessionNotFound, id)
	}
	records, err := session.LoadHistoryRecords(sessDir, id, true)
	if err != nil {
		return fmt.Errorf("load history %q: %w", id, err)
	}

	if *jsonOutput {
		payload := map[string]any{
			"thread_id": id,
			"session":   meta,
			"history":   records,
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal session: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("thread_id: %s\n", id)
	fmt.Printf("title: %s\n", meta.Title)
	if meta.Summary != "" {
		fmt.Printf("summary: %s\n", meta.Summary)
	}
	if meta.CWD != "" {
		fmt.Printf("cwd: %s\n", meta.CWD)
	}
	fmt.Printf("created: %s\n", meta.CreatedAt.Format(time.RFC3339))
	if meta.UpdatedAt.After(meta.CreatedAt) {
		fmt.Printf("updated: %s\n", meta.UpdatedAt.Format(time.RFC3339))
	}
	if meta.PinnedAt != nil {
		fmt.Printf("pinned: %s\n", meta.PinnedAt.Format(time.RFC3339))
	}
	if meta.ArchivedAt != nil {
		fmt.Printf("archived: %s\n", meta.ArchivedAt.Format(time.RFC3339))
	}
	if meta.ForkedFromID != "" {
		fmt.Printf("forked_from: %s\n", meta.ForkedFromID)
	}
	fmt.Printf("entries: %d\n", meta.Entries)
	fmt.Printf("history_records: %d\n\n", len(records))

	shown := *limit
	if shown > 0 && shown < len(records) {
		fmt.Printf("(showing first %d of %d records; use --limit 0 for all or --json for full output)\n\n", shown, len(records))
	}
	for i, rec := range records {
		if shown > 0 && i >= shown {
			break
		}
		prefix := "  "
		if rec.Steered {
			prefix = "* "
		}
		role := rec.Role
		if role == "" {
			role = "?"
		}
		content := rec.Content
		if len(content) > 240 {
			content = content[:240] + "..."
		}
		fmt.Printf("%s[%d] %s: %s\n", prefix, i+1, role, content)
	}
	return nil
}

func runSession(args []string) error {
	if len(args) == 0 {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("session subcommand is required"))
	}
	switch args[0] {
	case "list":
		return runSessionList(args[1:])
	case "show":
		return runSessionShowSubcommand(args[1:])
	case "trace":
		return runSessionTrace(args[1:])
	case "search":
		return runSessionSearch(args[1:])
	case "archive":
		return runSessionArchive(args[1:])
	case "delete":
		return runSessionDelete(args[1:])
	default:
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, fmt.Errorf("unknown session subcommand %q", args[0]))
	}
}

func runSessionList(args []string) error {
	fs := flag.NewFlagSet("session list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOutput := fs.Bool("json", false, "output as JSON")
	limit := fs.Int("limit", 50, "max sessions to print")
	workdir := fs.String("workdir", "", "workspace directory")
	allWorkdirs := fs.Bool("all-workdirs", false, "list sessions from every workspace")
	includeArchived := fs.Bool("include-archived", false, "include archived sessions")
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}

	sessDir, err := resolveSessionsDir()
	if err != nil {
		return err
	}
	var sessions []session.Session
	if *allWorkdirs {
		sessions, err = session.List(sessDir, *limit)
	} else {
		rootDir, werr := resolveWorkdir(*workdir)
		if werr != nil {
			return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, werr)
		}
		sessions, err = session.ListForCWD(sessDir, rootDir, *limit)
	}
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	sessions = filterArchivedSessions(sessions, *includeArchived)
	if *jsonOutput {
		return printJSON(map[string]any{"sessions": sessions})
	}
	for _, sess := range sessions {
		title := firstNonEmptyString(sess.Title, sess.Summary, sess.ID)
		fmt.Printf("%s\t%s\t%s\n", sess.ID, sess.UpdatedAt.Format(time.RFC3339), title)
	}
	return nil
}

func runSessionShowSubcommand(args []string) error {
	fs := flag.NewFlagSet("session show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOutput := fs.Bool("json", false, "output as JSON")
	limit := fs.Int("limit", 200, "max history records to print in human mode")
	threadFlag := fs.String("thread", "", "thread id; defaults to most recent for this workspace")
	workdir := fs.String("workdir", "", "workspace directory")
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	id := strings.TrimSpace(*threadFlag)
	if id == "" && len(fs.Args()) > 0 {
		id = strings.TrimSpace(fs.Args()[0])
	}
	sessDir, err := resolveSessionsDir()
	if err != nil {
		return err
	}
	rootDir, err := resolveWorkdir(*workdir)
	if err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	return printSession(sessDir, id, rootDir, *jsonOutput, *limit)
}

func runSessionTrace(args []string) error {
	fs := flag.NewFlagSet("session trace", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOutput := fs.Bool("json", false, "output as JSON")
	threadFlag := fs.String("thread", "", "thread id; defaults to most recent for this workspace")
	workdir := fs.String("workdir", "", "workspace directory")
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	id := strings.TrimSpace(*threadFlag)
	if id == "" && len(fs.Args()) > 0 {
		id = strings.TrimSpace(fs.Args()[0])
	}
	sessDir, err := resolveSessionsDir()
	if err != nil {
		return err
	}
	rootDir, err := resolveWorkdir(*workdir)
	if err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	if id == "" {
		id, err = session.MostRecentForCWD(sessDir, rootDir)
		if err != nil {
			return fmt.Errorf("find most recent session: %w", err)
		}
		if id == "" {
			return errors.New("no sessions found")
		}
	}
	meta, ok, err := session.Find(sessDir, id)
	if err != nil {
		return fmt.Errorf("lookup %q: %w", id, err)
	}
	if !ok {
		return fmt.Errorf("%w: %q", session.ErrSessionNotFound, id)
	}
	tracePath, err := tracePathForSession(meta, rootDir)
	if err != nil {
		return err
	}
	summary, err := sessiontrace.ReplayTrace(tracePath)
	if err != nil {
		return fmt.Errorf("replay trace %q: %w", tracePath, err)
	}
	if *jsonOutput {
		return printJSON(map[string]any{
			"thread_id":  id,
			"trace_path": tracePath,
			"summary":    summary,
		})
	}
	fmt.Printf("thread_id: %s\ntrace_path: %s\n", id, tracePath)
	if summary.LatestTurn != nil {
		fmt.Printf("latest_status: %s\n", summary.LatestTurn.Status)
	}
	if summary.Final != nil {
		fmt.Printf("final_status: %s\n", summary.Final.Status)
	}
	fmt.Printf("events: %d\n", summary.EventCount)
	return nil
}

type sessionSearchResult struct {
	ThreadID string          `json:"thread_id"`
	Session  session.Session `json:"session"`
	Snippet  string          `json:"snippet,omitempty"`
}

func runSessionSearch(args []string) error {
	fs := flag.NewFlagSet("session search", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOutput := fs.Bool("json", false, "output as JSON")
	limit := fs.Int("limit", 40, "max results to print")
	workdir := fs.String("workdir", "", "workspace directory")
	allWorkdirs := fs.Bool("all-workdirs", false, "search sessions from every workspace")
	includeArchived := fs.Bool("include-archived", false, "include archived sessions")
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("search query is required"))
	}
	sessDir, err := resolveSessionsDir()
	if err != nil {
		return err
	}
	var sessions []session.Session
	if *allWorkdirs {
		sessions, err = session.List(sessDir, 0)
	} else {
		rootDir, werr := resolveWorkdir(*workdir)
		if werr != nil {
			return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, werr)
		}
		sessions, err = session.ListForCWD(sessDir, rootDir, 0)
	}
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	results, err := searchSessions(sessDir, filterArchivedSessions(sessions, *includeArchived), query, *limit)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(map[string]any{"query": query, "results": results})
	}
	for _, result := range results {
		title := firstNonEmptyString(result.Session.Title, result.Session.Summary, result.ThreadID)
		if result.Snippet != "" {
			fmt.Printf("%s\t%s\t%s\t%s\n", result.ThreadID, result.Session.UpdatedAt.Format(time.RFC3339), title, result.Snippet)
			continue
		}
		fmt.Printf("%s\t%s\t%s\n", result.ThreadID, result.Session.UpdatedAt.Format(time.RFC3339), title)
	}
	return nil
}

func runSessionArchive(args []string) error {
	fs := flag.NewFlagSet("session archive", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOutput := fs.Bool("json", false, "output as JSON")
	unarchive := fs.Bool("unarchive", false, "mark session as active instead of archived")
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	if len(fs.Args()) == 0 || strings.TrimSpace(fs.Args()[0]) == "" {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("thread id is required"))
	}
	threadID := strings.TrimSpace(fs.Args()[0])
	sessDir, err := resolveSessionsDir()
	if err != nil {
		return err
	}
	meta, err := session.UpdateArchived(sessDir, threadID, !*unarchive)
	if err != nil {
		return fmt.Errorf("archive %q: %w", threadID, err)
	}
	if *jsonOutput {
		return printJSON(map[string]any{
			"thread_id": threadID,
			"session":   meta,
			"archived":  meta.ArchivedAt != nil,
		})
	}
	if meta.ArchivedAt != nil {
		fmt.Printf("archived: %s\n", threadID)
		return nil
	}
	fmt.Printf("unarchived: %s\n", threadID)
	return nil
}

func runSessionDelete(args []string) error {
	fs := flag.NewFlagSet("session delete", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOutput := fs.Bool("json", false, "output as JSON")
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	if len(fs.Args()) == 0 || strings.TrimSpace(fs.Args()[0]) == "" {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("thread id is required"))
	}
	threadID := strings.TrimSpace(fs.Args()[0])
	sessDir, err := resolveSessionsDir()
	if err != nil {
		return err
	}
	deleted, err := session.Delete(sessDir, threadID)
	if err != nil {
		return fmt.Errorf("delete %q: %w", threadID, err)
	}
	artifactPath, artifactsDeleted, err := deleteSessionArtifacts(deleted)
	if err != nil {
		return fmt.Errorf("delete artifacts for %q: %w", threadID, err)
	}
	if *jsonOutput {
		return printJSON(map[string]any{
			"thread_id":         threadID,
			"session":           deleted,
			"deleted":           true,
			"artifact_path":     artifactPath,
			"artifacts_deleted": artifactsDeleted,
		})
	}
	if artifactPath != "" && artifactsDeleted {
		fmt.Printf("deleted: %s\nartifacts: %s\n", threadID, artifactPath)
		return nil
	}
	fmt.Printf("deleted: %s\n", threadID)
	return nil
}

func deleteSessionArtifacts(sess session.Session) (string, bool, error) {
	if strings.TrimSpace(sess.CWD) == "" {
		return "", false, nil
	}
	home, err := statepath.Home("")
	if err != nil {
		return "", false, fmt.Errorf("resolve wuu home: %w", err)
	}
	workspaceStateDir, err := statepath.WorkspaceDir(home, sess.CWD)
	if err != nil {
		return "", false, fmt.Errorf("resolve workspace state: %w", err)
	}
	artifactPath := statepath.SessionArtifactDir(workspaceStateDir, sess.ID)
	if _, err := os.Stat(artifactPath); err != nil {
		if os.IsNotExist(err) {
			return artifactPath, false, nil
		}
		return artifactPath, false, err
	}
	if err := os.RemoveAll(artifactPath); err != nil {
		return artifactPath, false, err
	}
	return artifactPath, true, nil
}

func searchSessions(sessDir string, sessions []session.Session, query string, limit int) ([]sessionSearchResult, error) {
	normalizedQuery := normalizeSessionSearchText(query)
	if normalizedQuery == "" {
		return nil, nil
	}
	results := make([]sessionSearchResult, 0, minPositive(limit, len(sessions)))
	for _, sess := range sessions {
		snippet, err := sessionSearchSnippet(sessDir, sess, normalizedQuery)
		if err != nil {
			return nil, err
		}
		if snippet == "" {
			continue
		}
		results = append(results, sessionSearchResult{
			ThreadID: sess.ID,
			Session:  sess,
			Snippet:  snippet,
		})
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results, nil
}

func sessionSearchSnippet(sessDir string, sess session.Session, normalizedQuery string) (string, error) {
	for _, candidate := range []string{sess.Title, sess.Summary, sess.ID, sess.CWD} {
		if sessionSearchMatches(candidate, normalizedQuery) {
			return compactSessionSearchSnippet(candidate), nil
		}
	}
	records, err := session.LoadHistoryRecords(sessDir, sess.ID, true)
	if err != nil {
		return "", fmt.Errorf("load history %q: %w", sess.ID, err)
	}
	for _, rec := range records {
		for _, candidate := range []string{rec.Content, rec.Name, rec.ToolCallID} {
			if sessionSearchMatches(candidate, normalizedQuery) {
				return compactSessionSearchSnippet(candidate), nil
			}
		}
	}
	return "", nil
}

func sessionSearchMatches(value, normalizedQuery string) bool {
	return strings.Contains(normalizeSessionSearchText(value), normalizedQuery)
}

func normalizeSessionSearchText(value string) string {
	return strings.ToLower(compactSessionSearchSnippet(value))
}

func compactSessionSearchSnippet(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func minPositive(a, b int) int {
	if a <= 0 {
		return b
	}
	if b <= 0 || a < b {
		return a
	}
	return b
}

func printSession(sessDir, id, rootDir string, jsonOutput bool, limit int) error {
	var err error
	id = strings.TrimSpace(id)
	if id == "" {
		id, err = session.MostRecentForCWD(sessDir, rootDir)
		if err != nil {
			return fmt.Errorf("find most recent session: %w", err)
		}
		if id == "" {
			return errors.New("no sessions found")
		}
	}

	meta, ok, err := session.Find(sessDir, id)
	if err != nil {
		return fmt.Errorf("lookup %q: %w", id, err)
	}
	if !ok {
		return fmt.Errorf("%w: %q", session.ErrSessionNotFound, id)
	}
	records, err := session.LoadHistoryRecords(sessDir, id, true)
	if err != nil {
		return fmt.Errorf("load history %q: %w", id, err)
	}
	if jsonOutput {
		return printJSON(map[string]any{
			"thread_id": id,
			"session":   meta,
			"history":   records,
		})
	}

	fmt.Printf("thread_id: %s\n", id)
	fmt.Printf("title: %s\n", meta.Title)
	if meta.Summary != "" {
		fmt.Printf("summary: %s\n", meta.Summary)
	}
	if meta.CWD != "" {
		fmt.Printf("cwd: %s\n", meta.CWD)
	}
	fmt.Printf("created: %s\n", meta.CreatedAt.Format(time.RFC3339))
	if meta.UpdatedAt.After(meta.CreatedAt) {
		fmt.Printf("updated: %s\n", meta.UpdatedAt.Format(time.RFC3339))
	}
	fmt.Printf("entries: %d\nhistory_records: %d\n\n", meta.Entries, len(records))
	shown := limit
	if shown > 0 && shown < len(records) {
		fmt.Printf("(showing first %d of %d records; use --limit 0 for all or --json for full output)\n\n", shown, len(records))
	}
	for i, rec := range records {
		if shown > 0 && i >= shown {
			break
		}
		role := firstNonEmptyString(rec.Role, "?")
		content := rec.Content
		if len(content) > 240 {
			content = content[:240] + "..."
		}
		fmt.Printf("  [%d] %s: %s\n", i+1, role, content)
	}
	return nil
}

func resolveSessionsDir() (string, error) {
	home, err := statepath.Home("")
	if err != nil {
		return "", fmt.Errorf("resolve wuu home: %w", err)
	}
	sessDir := statepath.SessionsDir(home)
	if sessDir == "" {
		return "", errors.New("sessions dir is empty")
	}
	return sessDir, nil
}

func filterArchivedSessions(sessions []session.Session, includeArchived bool) []session.Session {
	if includeArchived {
		return sessions
	}
	filtered := sessions[:0]
	for _, sess := range sessions {
		if sess.ArchivedAt == nil {
			filtered = append(filtered, sess)
		}
	}
	return filtered
}

func tracePathForSession(sess session.Session, fallbackRoot string) (string, error) {
	rootDir := strings.TrimSpace(sess.CWD)
	if rootDir == "" {
		rootDir = fallbackRoot
	}
	if rootDir == "" {
		return "", errors.New("session has no workspace cwd; pass --workdir")
	}
	home, err := statepath.Home("")
	if err != nil {
		return "", fmt.Errorf("resolve wuu home: %w", err)
	}
	workspaceStateDir, err := statepath.WorkspaceDir(home, rootDir)
	if err != nil {
		return "", fmt.Errorf("resolve workspace state: %w", err)
	}
	return sessiontrace.Path(statepath.SessionArtifactDir(workspaceStateDir, sess.ID)), nil
}

func printJSON(payload any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type debugAppServerClient interface {
	Call(context.Context, string, any, any) error
	Shutdown(context.Context) error
}

var debugAppServerClientOverride func(context.Context, debugAppServerOptions) (debugAppServerClient, error)

type debugAppServerOptions struct {
	workdir  string
	provider string
	model    string
	noTools  bool
}

type debugAppServerCLIConfig struct {
	workdir  *string
	provider *string
	model    *string
	noTools  *bool
}

func runDebug(args []string) error {
	if len(args) == 0 {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("debug subcommand is required"))
	}
	switch args[0] {
	case "app-server":
		return runDebugAppServer(args[1:])
	case "protocol":
		return runDebugProtocol(args[1:])
	default:
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, fmt.Errorf("unknown debug subcommand %q", args[0]))
	}
}

func runDebugAppServer(args []string) error {
	if len(args) == 0 {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("debug app-server subcommand is required"))
	}
	switch args[0] {
	case "initialize":
		return runDebugAppServerInitialize(args[1:])
	case "send":
		return runDebugAppServerSend(args[1:])
	default:
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, fmt.Errorf("unknown debug app-server subcommand %q", args[0]))
	}
}

func runDebugAppServerInitialize(args []string) error {
	fs := flag.NewFlagSet("debug app-server initialize", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := addDebugAppServerFlags(fs)
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	client, err := newDebugAppServerClient(context.Background(), debugAppServerOptionsFromCLI(cfg))
	if err != nil {
		return err
	}
	defer shutdownDebugClient(client)

	var result json.RawMessage
	if err := client.Call(context.Background(), appserver.MethodInitialize, nil, &result); err != nil {
		return err
	}
	return printRawJSON(result)
}

func runDebugAppServerSend(args []string) error {
	fs := flag.NewFlagSet("debug app-server send", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := addDebugAppServerFlags(fs)
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	remaining := fs.Args()
	if len(remaining) == 0 || strings.TrimSpace(remaining[0]) == "" {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("method is required"))
	}
	method := strings.TrimSpace(remaining[0])
	params, err := parseDebugJSONParams(remaining[1:])
	if err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	client, err := newDebugAppServerClient(context.Background(), debugAppServerOptionsFromCLI(cfg))
	if err != nil {
		return err
	}
	defer shutdownDebugClient(client)

	var result json.RawMessage
	if err := client.Call(context.Background(), method, params, &result); err != nil {
		return err
	}
	return printRawJSON(result)
}

func addDebugAppServerFlags(fs *flag.FlagSet) debugAppServerCLIConfig {
	return debugAppServerCLIConfig{
		workdir:  fs.String("workdir", "", "workspace directory"),
		provider: fs.String("provider", "", "provider name in config"),
		model:    fs.String("model", "", "model override"),
		noTools:  fs.Bool("no-tools", false, "disable local tools"),
	}
}

func debugAppServerOptionsFromCLI(cfg debugAppServerCLIConfig) debugAppServerOptions {
	return debugAppServerOptions{
		workdir:  valueOfStringFlag(cfg.workdir),
		provider: valueOfStringFlag(cfg.provider),
		model:    valueOfStringFlag(cfg.model),
		noTools:  valueOfBoolFlag(cfg.noTools),
	}
}

func newDebugAppServerClient(ctx context.Context, opts debugAppServerOptions) (debugAppServerClient, error) {
	if debugAppServerClientOverride != nil {
		return debugAppServerClientOverride(ctx, opts)
	}
	return newLocalDebugAppServerClient(ctx, opts)
}

type localDebugAppServerClient struct {
	rt     *runtime.Session
	client *wuuexec.ProtocolClient
	cancel context.CancelFunc
	done   chan error
	pipes  []io.Closer
}

func newLocalDebugAppServerClient(ctx context.Context, opts debugAppServerOptions) (*localDebugAppServerClient, error) {
	rootDir, err := resolveWorkdir(opts.workdir)
	if err != nil {
		return nil, wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	homeDir := os.Getenv("HOME")
	cfg, configPath, err := loadOrCreateAppServerConfig(rootDir, homeDir)
	if err != nil {
		return nil, err
	}
	rt, err := runtime.NewSession(runtime.Options{
		RootDir:       rootDir,
		HomeDir:       homeDir,
		ConfigPath:    configPath,
		Config:        cfg,
		ProviderName:  opts.provider,
		ModelOverride: opts.model,
		NoTools:       opts.noTools,
	})
	if err != nil {
		return nil, err
	}
	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()
	serverCtx, cancel := context.WithCancel(ctx)
	client := &localDebugAppServerClient{
		rt:     rt,
		client: wuuexec.NewProtocolClient(serverOutR, serverInW),
		cancel: cancel,
		done:   make(chan error, 1),
		pipes:  []io.Closer{serverInR, serverInW, serverOutR, serverOutW},
	}
	go func() {
		client.done <- appserver.RunStdio(serverCtx, rt, serverInR, serverOutW)
	}()
	return client, nil
}

func (c *localDebugAppServerClient) Call(ctx context.Context, method string, params any, result any) error {
	return c.client.Call(ctx, method, params, result)
}

func (c *localDebugAppServerClient) Shutdown(ctx context.Context) error {
	if c.cancel != nil {
		defer c.cancel()
	}
	var result appserver.OKResult
	err := c.client.Call(ctx, appserver.MethodShutdown, nil, &result)
	for _, pipe := range c.pipes {
		_ = pipe.Close()
	}
	if c.rt != nil {
		_, _ = c.rt.Cleanup()
	}
	select {
	case runErr := <-c.done:
		if err == nil && runErr != nil && !errors.Is(runErr, io.ErrClosedPipe) {
			err = runErr
		}
	default:
	}
	return err
}

func shutdownDebugClient(client debugAppServerClient) {
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = client.Shutdown(ctx)
}

func parseDebugJSONParams(args []string) (json.RawMessage, error) {
	if len(args) == 0 {
		return nil, nil
	}
	raw := strings.TrimSpace(strings.Join(args, " "))
	if raw == "" {
		return nil, nil
	}
	var params json.RawMessage
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return nil, fmt.Errorf("parse params JSON: %w", err)
	}
	return params, nil
}

func runDebugProtocol(args []string) error {
	if len(args) == 0 {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("debug protocol subcommand is required"))
	}
	switch args[0] {
	case "events":
		return runDebugProtocolEvents(args[1:])
	default:
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, fmt.Errorf("unknown debug protocol subcommand %q", args[0]))
	}
}

func runDebugProtocolEvents(args []string) error {
	fs := flag.NewFlagSet("debug protocol events", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOutput := fs.Bool("json", false, "output as one JSON object")
	workdir := fs.String("workdir", "", "workspace directory")
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	remaining := fs.Args()
	if len(remaining) == 0 || strings.TrimSpace(remaining[0]) == "" {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("thread id is required"))
	}
	threadID := strings.TrimSpace(remaining[0])
	sessDir, err := resolveSessionsDir()
	if err != nil {
		return err
	}
	rootDir, err := resolveWorkdir(*workdir)
	if err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	meta, ok, err := session.Find(sessDir, threadID)
	if err != nil {
		return fmt.Errorf("lookup %q: %w", threadID, err)
	}
	if !ok {
		return fmt.Errorf("%w: %q", session.ErrSessionNotFound, threadID)
	}
	tracePath, err := tracePathForSession(meta, rootDir)
	if err != nil {
		return err
	}
	events, err := readTraceEvents(tracePath)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(map[string]any{
			"thread_id":  threadID,
			"trace_path": tracePath,
			"events":     events,
		})
	}
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal trace event: %w", err)
		}
		fmt.Println(string(data))
	}
	return nil
}

func readTraceEvents(path string) ([]json.RawMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open trace %q: %w", path, err)
	}
	defer file.Close()

	var events []json.RawMessage
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var event json.RawMessage
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, fmt.Errorf("decode trace line %d: %w", line, err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read trace %q: %w", path, err)
	}
	return events, nil
}

func printRawJSON(raw json.RawMessage) error {
	if len(raw) == 0 {
		fmt.Println("null")
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("decode JSON result: %w", err)
	}
	return printJSON(value)
}

type execCLIConfig struct {
	provider          *string
	model             *string
	effort            *string
	variant           *string
	permissionMode    *string
	workdir           *string
	configPath        *string
	agentProfile      *string
	ignoreUserConfig  *bool
	strictConfig      *bool
	env               *stringListFlag
	files             *stringListFlag
	images            *stringListFlag
	noTools           *bool
	jsonOutput        *bool
	timeout           *time.Duration
	ephemeral         *bool
	outputLastMessage *string
	inputJSON         *bool
	maxTurns          *int
	outputSchema      *string
}

var execControllerOverride wuuexec.Controller

func runLegacyRun(args []string) error {
	if flagName, ok := firstLegacyRunOnlyFlag(args); ok {
		return wuuexec.WithExitCode(
			wuuexec.ExitInvalidInput,
			fmt.Errorf("wuu run is now a compatibility wrapper around wuu exec; %s is not supported by the app-server path", flagName),
		)
	}
	return runExec(args)
}

func firstLegacyRunOnlyFlag(args []string) (string, bool) {
	for _, arg := range args {
		if arg == "--" || arg == "-" || !strings.HasPrefix(arg, "-") {
			return "", false
		}
		name := strings.TrimLeft(arg, "-")
		if name == "" {
			return "", false
		}
		name, _, _ = strings.Cut(name, "=")
		switch name {
		case "max-steps", "temperature", "system-prompt":
			return "--" + name, true
		}
	}
	return "", false
}

func runExec(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "resume":
			return runExecResume(args[1:])
		case "fork":
			return runExecFork(args[1:])
		case "review":
			return runExecReview(args[1:])
		}
	}

	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := addExecFlags(fs)
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	if err := validateExecFlags(cfg); err != nil {
		return err
	}
	prompt, input, err := resolveExecPromptAndInput(cfg, fs.Args(), hasExecAttachments(cfg))
	if err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	opts, err := execOptionsFromCLI(cfg, prompt, "", false, input)
	if err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	return runExecWithPrompt(prompt, opts)
}

func runExecResume(args []string) error {
	fs := flag.NewFlagSet("exec resume", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := addExecFlags(fs)
	last := fs.Bool("last", false, "resume the most recent session for this workspace")
	all := fs.Bool("all", false, "resume with full available session context")
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	if *all {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("wuu exec resume --all is not implemented yet"))
	}
	if err := validateExecFlags(cfg); err != nil {
		return err
	}

	remaining := fs.Args()
	threadID := ""
	if !*last {
		if len(remaining) == 0 {
			return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("resume requires --last or a thread id"))
		}
		threadID = strings.TrimSpace(remaining[0])
		remaining = remaining[1:]
	}
	prompt, input, err := resolveExecPromptAndInput(cfg, remaining, hasExecAttachments(cfg))
	if err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	opts, err := execOptionsFromCLI(cfg, prompt, threadID, *last, input)
	if err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	return runExecWithPrompt(prompt, opts)
}

func runExecFork(args []string) error {
	fs := flag.NewFlagSet("exec fork", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := addExecFlags(fs)
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	if err := validateExecFlags(cfg); err != nil {
		return err
	}
	remaining := fs.Args()
	if len(remaining) == 0 {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("fork requires a thread id"))
	}
	forkID := strings.TrimSpace(remaining[0])
	if forkID == "" {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("fork requires a thread id"))
	}
	prompt, input, err := resolveExecPromptAndInput(cfg, remaining[1:], hasExecAttachments(cfg))
	if err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	opts, err := execOptionsFromCLI(cfg, prompt, "", false, input)
	if err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	opts.ForkID = forkID
	return runExecWithPrompt(prompt, opts)
}

func runExecReview(args []string) error {
	fs := flag.NewFlagSet("exec review", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := addExecFlags(fs)
	uncommitted := fs.Bool("uncommitted", false, "review current uncommitted changes")
	base := fs.String("base", "", "review changes against base ref")
	commit := fs.String("commit", "", "review one commit")
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	if err := validateExecFlags(cfg); err != nil {
		return err
	}
	if valueOfBoolFlag(cfg.inputJSON) {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("wuu exec review does not support --input-json"))
	}
	prompt, err := reviewPromptFromFlags(*uncommitted, *base, *commit, fs.Args())
	if err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	opts, err := execOptionsFromCLI(cfg, prompt, "", false, nil)
	if err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	return runExecWithPrompt(prompt, opts)
}

func reviewPromptFromFlags(uncommitted bool, base, commit string, extraArgs []string) (string, error) {
	base = strings.TrimSpace(base)
	commit = strings.TrimSpace(commit)
	selected := 0
	if uncommitted {
		selected++
	}
	if base != "" {
		selected++
	}
	if commit != "" {
		selected++
	}
	if selected != 1 {
		return "", errors.New("review requires exactly one of --uncommitted, --base, or --commit")
	}

	var prompt string
	switch {
	case uncommitted:
		prompt = strings.Join([]string{
			"Review the current uncommitted changes in this repository.",
			"Inspect `git status --short`, `git diff --stat`, and `git diff` before reporting findings.",
		}, "\n")
	case base != "":
		prompt = fmt.Sprintf("Review the changes in this repository against base ref `%s`.\nInspect the merge-base diff before reporting findings.", base)
	case commit != "":
		prompt = fmt.Sprintf("Review commit `%s` in this repository.\nInspect `git show --stat --patch %s` before reporting findings.", commit, commit)
	}
	prompt += "\n\nFocus only on real bugs, security issues, behavior regressions, and missing tests that matter. Do not nitpick style."

	if extra := strings.TrimSpace(strings.Join(extraArgs, " ")); extra != "" {
		prompt += "\n\nAdditional instructions:\n" + extra
	}
	return prompt, nil
}

func addExecFlags(fs *flag.FlagSet) execCLIConfig {
	files := stringListFlag{}
	images := stringListFlag{}
	env := stringListFlag{}
	fs.Var(&files, "file", "attach a local file (repeatable)")
	fs.Var(&images, "image", "attach a local image (repeatable)")
	fs.Var(&env, "env", "set an environment variable for the run (KEY=VALUE, repeatable)")
	return execCLIConfig{
		provider:          fs.String("provider", "", "provider name in config"),
		model:             fs.String("model", "", "model override"),
		effort:            fs.String("effort", "", "reasoning effort override"),
		variant:           fs.String("variant", "", "model variant override"),
		permissionMode:    fs.String("permission-mode", "", "permission mode override"),
		workdir:           fs.String("workdir", "", "workspace directory"),
		configPath:        fs.String("config", "", "explicit config file path"),
		agentProfile:      fs.String("profile", "", "agent profile name"),
		ignoreUserConfig:  fs.Bool("ignore-user-config", false, "ignore user-level config"),
		strictConfig:      fs.Bool("strict-config", false, "fail if config cannot be loaded"),
		env:               &env,
		files:             &files,
		images:            &images,
		noTools:           fs.Bool("no-tools", false, "disable local tools"),
		jsonOutput:        fs.Bool("json", false, "emit machine-readable JSONL to stdout"),
		timeout:           fs.Duration("timeout", 0, "total timeout (e.g. 20m)"),
		ephemeral:         fs.Bool("ephemeral", false, "run without creating a persistent session"),
		outputLastMessage: fs.String("output-last-message", "", "write final agent message to a file"),
		inputJSON:         fs.Bool("input-json", false, "read machine input JSON from stdin"),
		maxTurns:          fs.Int("max-turns", 0, "max agent turns"),
		outputSchema:      fs.String("output-schema", "", "JSON schema for structured final output"),
	}
}

func validateExecFlags(cfg execCLIConfig) error {
	for _, path := range append(stringListValues(cfg.files), stringListValues(cfg.images)...) {
		if strings.TrimSpace(path) == "" {
			return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("attachment path is required"))
		}
	}
	if cfg.maxTurns != nil && *cfg.maxTurns > 0 {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("wuu exec --max-turns is not implemented yet"))
	}
	if cfg.outputSchema != nil && strings.TrimSpace(*cfg.outputSchema) != "" {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("wuu exec --output-schema is not implemented yet"))
	}
	return nil
}

type execInputPayload struct {
	Prompt            string                     `json:"prompt"`
	Stdin             string                     `json:"stdin"`
	Files             []string                   `json:"files"`
	Images            []string                   `json:"images"`
	FileAttachments   []appserver.TurnStartFile  `json:"file_attachments"`
	ImageAttachments  []appserver.TurnStartImage `json:"image_attachments"`
	Workdir           string                     `json:"workdir"`
	Provider          string                     `json:"provider"`
	Model             string                     `json:"model"`
	Effort            string                     `json:"effort"`
	Variant           string                     `json:"variant"`
	PermissionMode    string                     `json:"permission_mode"`
	ConfigPath        string                     `json:"config"`
	AgentProfile      string                     `json:"profile"`
	IgnoreUserConfig  *bool                      `json:"ignore_user_config"`
	StrictConfig      *bool                      `json:"strict_config"`
	Env               []string                   `json:"env"`
	NoTools           *bool                      `json:"no_tools"`
	JSON              *bool                      `json:"json"`
	Ephemeral         *bool                      `json:"ephemeral"`
	Timeout           string                     `json:"timeout"`
	OutputLastMessage string                     `json:"output_last_message"`
}

func execOptionsFromCLI(cfg execCLIConfig, prompt, resumeID string, resumeLast bool, input *execInputPayload) (wuuexec.Options, error) {
	opts := wuuexec.Options{
		Prompt:            prompt,
		ImagePaths:        stringListValues(cfg.images),
		FilePaths:         stringListValues(cfg.files),
		Provider:          valueOfStringFlag(cfg.provider),
		Model:             valueOfStringFlag(cfg.model),
		Effort:            valueOfStringFlag(cfg.effort),
		Variant:           valueOfStringFlag(cfg.variant),
		PermissionMode:    valueOfStringFlag(cfg.permissionMode),
		Workdir:           valueOfStringFlag(cfg.workdir),
		ConfigPath:        valueOfStringFlag(cfg.configPath),
		AgentProfile:      valueOfStringFlag(cfg.agentProfile),
		IgnoreUserConfig:  valueOfBoolFlag(cfg.ignoreUserConfig),
		StrictConfig:      valueOfBoolFlag(cfg.strictConfig),
		Env:               stringListValues(cfg.env),
		NoTools:           valueOfBoolFlag(cfg.noTools),
		JSON:              valueOfBoolFlag(cfg.jsonOutput),
		Ephemeral:         valueOfBoolFlag(cfg.ephemeral),
		Timeout:           valueOfDurationFlag(cfg.timeout),
		OutputLastMessage: valueOfStringFlag(cfg.outputLastMessage),
		ResumeID:          resumeID,
		ResumeLast:        resumeLast,
		Stdout:            os.Stdout,
		Stderr:            os.Stderr,
		Controller:        execControllerOverride,
	}
	if err := applyExecInputPayload(&opts, input); err != nil {
		return wuuexec.Options{}, err
	}
	return opts, nil
}

func applyExecInputPayload(opts *wuuexec.Options, input *execInputPayload) error {
	if opts == nil || input == nil {
		return nil
	}
	opts.FilePaths = append(opts.FilePaths, input.Files...)
	opts.ImagePaths = append(opts.ImagePaths, input.Images...)
	opts.Attachments.Files = append(opts.Attachments.Files, input.FileAttachments...)
	opts.Attachments.Images = append(opts.Attachments.Images, input.ImageAttachments...)
	if opts.Workdir == "" {
		opts.Workdir = strings.TrimSpace(input.Workdir)
	}
	if opts.Provider == "" {
		opts.Provider = strings.TrimSpace(input.Provider)
	}
	if opts.Model == "" {
		opts.Model = strings.TrimSpace(input.Model)
	}
	if opts.Effort == "" {
		opts.Effort = strings.TrimSpace(input.Effort)
	}
	if opts.Variant == "" {
		opts.Variant = strings.TrimSpace(input.Variant)
	}
	if opts.PermissionMode == "" {
		opts.PermissionMode = strings.TrimSpace(input.PermissionMode)
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = strings.TrimSpace(input.ConfigPath)
	}
	if opts.AgentProfile == "" {
		opts.AgentProfile = strings.TrimSpace(input.AgentProfile)
	}
	if input.IgnoreUserConfig != nil && !opts.IgnoreUserConfig {
		opts.IgnoreUserConfig = *input.IgnoreUserConfig
	}
	if input.StrictConfig != nil && !opts.StrictConfig {
		opts.StrictConfig = *input.StrictConfig
	}
	opts.Env = append(opts.Env, input.Env...)
	if input.NoTools != nil && !opts.NoTools {
		opts.NoTools = *input.NoTools
	}
	if input.JSON != nil && !opts.JSON {
		opts.JSON = *input.JSON
	}
	if input.Ephemeral != nil && !opts.Ephemeral {
		opts.Ephemeral = *input.Ephemeral
	}
	if opts.OutputLastMessage == "" {
		opts.OutputLastMessage = strings.TrimSpace(input.OutputLastMessage)
	}
	if opts.Timeout == 0 && strings.TrimSpace(input.Timeout) != "" {
		timeout, err := time.ParseDuration(strings.TrimSpace(input.Timeout))
		if err != nil {
			return fmt.Errorf("parse input_json.timeout: %w", err)
		}
		opts.Timeout = timeout
	}
	return nil
}

func runExecWithPrompt(prompt string, opts wuuexec.Options) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return wuuexec.Run(ctx, opts)
}

func resolveExecPromptAndInput(cfg execCLIConfig, args []string, allowEmpty bool) (string, *execInputPayload, error) {
	if !valueOfBoolFlag(cfg.inputJSON) {
		prompt, err := resolveExecPrompt(args, allowEmpty)
		return prompt, nil, err
	}
	if len(args) > 0 {
		return "", nil, errors.New("positional prompt is not allowed with --input-json")
	}
	input, err := readExecInputPayload(os.Stdin, stdinHasInput())
	if err != nil {
		return "", nil, err
	}
	prompt := input.promptText()
	if prompt == "" && !allowEmpty && !input.hasAttachments() {
		return "", nil, errors.New("prompt is required in --input-json input")
	}
	return prompt, input, nil
}

func resolveExecPrompt(args []string, allowEmpty bool) (string, error) {
	return wuuexec.ResolvePrompt(wuuexec.PromptInput{
		Args:        args,
		Stdin:       os.Stdin,
		StdinIsPipe: stdinHasInput(),
		AllowEmpty:  allowEmpty,
	})
}

func readExecInputPayload(r io.Reader, stdinIsPipe bool) (*execInputPayload, error) {
	if !stdinIsPipe {
		return nil, errors.New("--input-json requires JSON on stdin")
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read input JSON: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, errors.New("--input-json input is empty")
	}
	var input execInputPayload
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, fmt.Errorf("decode input JSON: %w", err)
	}
	return &input, nil
}

func (p *execInputPayload) promptText() string {
	if p == nil {
		return ""
	}
	prompt := strings.TrimSpace(p.Prompt)
	stdinText := strings.TrimSpace(p.Stdin)
	if prompt != "" && stdinText != "" {
		return prompt + "\n\n<stdin>\n" + stdinText + "\n</stdin>"
	}
	if prompt != "" {
		return prompt
	}
	return stdinText
}

func (p *execInputPayload) hasAttachments() bool {
	return p != nil && (len(p.Files) > 0 || len(p.Images) > 0 || len(p.FileAttachments) > 0 || len(p.ImageAttachments) > 0)
}

func stringListValues(f *stringListFlag) []string {
	if f == nil || len(*f) == 0 {
		return nil
	}
	return append([]string(nil), (*f)...)
}

func hasExecAttachments(cfg execCLIConfig) bool {
	return len(stringListValues(cfg.files)) > 0 || len(stringListValues(cfg.images)) > 0
}

func valueOfStringFlag(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func valueOfBoolFlag(v *bool) bool {
	return v != nil && *v
}

func valueOfDurationFlag(v *time.Duration) time.Duration {
	if v == nil {
		return 0
	}
	return *v
}

func runAppServer(args []string) error {
	fs := flag.NewFlagSet("app-server", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	providerName := fs.String("provider", "", "provider name in config")
	modelOverride := fs.String("model", "", "model override")
	workdir := fs.String("workdir", "", "workspace directory")
	noTools := fs.Bool("no-tools", false, "disable local tools")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rootDir, err := resolveWorkdir(*workdir)
	if err != nil {
		return err
	}
	homeDir := os.Getenv("HOME")
	cfg, configPath, err := loadOrCreateAppServerConfig(rootDir, homeDir)
	if err != nil {
		return err
	}

	rt, err := runtime.NewSession(runtime.Options{
		RootDir:       rootDir,
		HomeDir:       homeDir,
		ConfigPath:    configPath,
		Config:        cfg,
		ProviderName:  *providerName,
		ModelOverride: *modelOverride,
		NoTools:       *noTools,
	})
	if err != nil {
		return err
	}
	if err := rt.StartCronScheduler(); err != nil {
		_, _ = rt.Cleanup()
		return err
	}
	defer func() {
		_, _ = rt.Cleanup()
	}()

	return appserver.RunStdio(context.Background(), rt, os.Stdin, os.Stdout)
}

func loadOrCreateAppServerConfig(rootDir, homeDir string) (config.Config, string, error) {
	cfg, configPath, err := config.LoadFrom(rootDir, homeDir)
	if err == nil {
		return cfg, configPath, nil
	}
	if !errors.Is(err, config.ErrConfigNotFound) {
		return config.Config{}, "", err
	}

	configPath = filepath.Join(rootDir, ".wuu.json")
	if strings.TrimSpace(homeDir) != "" {
		configPath = filepath.Join(homeDir, ".config", "wuu", "config.json")
	}
	cfg = appServerStarterConfig()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return config.Config{}, "", err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return config.Config{}, "", fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(configPath, append(data, '\n'), 0o600); err != nil {
		return config.Config{}, "", fmt.Errorf("write starter config: %w", err)
	}
	return cfg, configPath, nil
}

func appServerStarterConfig() config.Config {
	cfg := config.Default()
	if provider, ok := cfg.Providers["openai-codex"]; ok {
		cfg.DefaultProvider = "openai-codex"
		cfg.Providers = map[string]config.ProviderConfig{
			"openai-codex": provider,
		}
	}
	return cfg
}

func resolveWorkdir(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get current directory: %w", err)
		}
		return cwd, nil
	}

	abs, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("resolve workdir: %w", err)
	}
	return abs, nil
}

func isCodexModelsProvider(providerType string) bool {
	s := strings.ToLower(strings.TrimSpace(providerType))
	s = strings.ReplaceAll(s, "_", "-")
	return s == "openai-codex" || s == "codex-subscription" || s == "chatgpt-codex"
}

func explicitProviderAPIKey(provider config.ProviderConfig) string {
	if key := strings.TrimSpace(provider.APIKey); key != "" {
		return key
	}
	if envKey := strings.TrimSpace(provider.APIKeyEnv); envKey != "" {
		return strings.TrimSpace(os.Getenv(envKey))
	}
	return ""
}

func resolveRuntimePath(rootDir, input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", nil
	}
	if filepath.IsAbs(value) {
		return value, nil
	}
	return filepath.Join(rootDir, value), nil
}

func stdinHasInput() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}

func printUsage() {
	fmt.Println(`wuu - GUI-first coding agent backend and CLI tools

Usage:
  wuu init [--force]
  wuu models [flags]
  wuu exec [flags] "your coding task"
  wuu exec resume (--last|THREAD_ID) [flags] "continue task"
  wuu exec fork THREAD_ID [flags] "continue from a fork"
  wuu exec review (--uncommitted|--base REF|--commit SHA) [flags]
  wuu session list|show|trace|search|archive|delete [flags]
  wuu debug app-server initialize [flags]
  wuu debug app-server send [flags] METHOD [JSON]
  wuu debug protocol events [flags] THREAD_ID
  wuu run [flags] "your coding task"
  wuu eval [flags]
  wuu goal demo [flags]
  wuu goal status [flags]
  wuu app-server [flags]
  wuu probe-title [flags]   run the LLM title pipeline against a real provider
  wuu version [--long|--json]

Models flags:
  --provider        provider name from config
  --workdir         workspace directory
  --json            output model metadata as JSON

Exec flags:
  --provider        provider name from config
  --model           model override
  --effort          reasoning effort override
  --variant         model variant override
  --permission-mode permission mode override
  --workdir         workspace directory
  --config          explicit config file path
  --profile         agent profile name
  --ignore-user-config
                   ignore user-level config
  --strict-config   fail if config cannot be loaded
  --env KEY=VALUE   set environment variable for the run (repeatable)
  --file            attach a local file (repeatable)
  --image           attach a local image (repeatable)
  --no-tools        disable local tools
  --json            emit JSONL to stdout
  --ephemeral       run without creating a persistent session
  --input-json      read machine input JSON from stdin
  --timeout         total timeout (e.g. 20m)
  --output-last-message
                   write final agent message to a file

Exec review:
  --uncommitted     review current uncommitted changes
  --base REF        review changes against base ref
  --commit SHA      review one commit

Session commands:
  list --json [--workdir DIR] [--all-workdirs]
                   list visible sessions for the workspace
  show --json [THREAD_ID] [--workdir DIR]
                   show session metadata and history
  trace --json [THREAD_ID] [--workdir DIR]
                   replay a session trace artifact
  search --json QUERY [--workdir DIR]
                   search session metadata and history
  archive [--json] THREAD_ID
                   hide a session from default lists
  delete [--json] THREAD_ID
                   delete a session and its workspace artifacts

Debug commands:
  app-server initialize [--workdir DIR] [--provider NAME] [--model MODEL] [--no-tools]
                   start a local app-server and print its initialize result
  app-server send [flags] METHOD [JSON]
                   send one app-server method and print the raw JSON result
  protocol events [--json] [--workdir DIR] THREAD_ID
                   print trace JSONL events recorded for a session

Legacy run:
  wuu run forwards to wuu exec. Use Exec flags for new automation.
  Legacy-only flags --max-steps, --temperature, and --system-prompt are not
  supported by the app-server path.

Eval flags:
  --provider        provider name from config
  --model           model override
  --workdir         workspace directory containing wuu config
  --task            task id, comma-separated ids, or all
  --list            list built-in eval tasks
  --json            output eval report as JSON
  --output          write eval report JSON to path
  --max-steps       max tool loop steps per task
  --timeout         timeout per task (default 10m)
  --keep-workdirs   keep temporary task workdirs
  --replay-trace    replay an eval trace JSONL without calling a model or tools
  --live-codex-oauth
                   run live MCP E2E with local Codex OAuth

Goal flags:
  demo --workdir DIR --goal TEXT [--id ID] [--verify-command CMD]
                   write a durable demo goal under Wuu workspace state
  status --workdir DIR [--id ID] [--json]
                   read goal state from Wuu workspace state

App server flags:
  --provider        provider name from config
  --model           model override
  --workdir         workspace directory
  --no-tools        disable local tools

Probe-title flags:
  --workdir         workspace directory (default: cwd)
  --thread          thread id to regenerate title for (default: most recent)
  --user-prompt     synthetic first user message; auto dry-run
  --provider        override provider from config
  --model           override model from config
  --dry-run         do not persist the title
  --verbose         print every step in human-readable mode
  --json            emit the result struct as JSON
  --quiet           suppress human-readable summary`)
}
