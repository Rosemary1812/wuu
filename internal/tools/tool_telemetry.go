package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

const (
	workspaceRevisionTimeout       = 500 * time.Millisecond
	workspaceDigestMaxFiles        = 5000
	workspaceDigestMaxBytes        = 32 * 1024 * 1024
	workspaceDigestMaxBytesPerFile = 1024 * 1024
)

// ToolExecutionRecord captures benchmark-oriented facts about one tool
// execution. It deliberately excludes arguments and output content.
type ToolExecutionRecord struct {
	Name                 string           `json:"name"`
	CallID               string           `json:"call_id,omitempty"`
	ArgumentsSHA256      string           `json:"arguments_sha256,omitempty"`
	Kind                 ToolKind         `json:"kind"`
	Exposure             ToolExposure     `json:"exposure"`
	Risk                 ToolRisk         `json:"risk"`
	ClassificationReason string           `json:"classification_reason,omitempty"`
	PolicyAction         ToolPolicyAction `json:"policy_action"`
	PolicyReason         string           `json:"policy_reason,omitempty"`
	ReadOnly             bool             `json:"read_only"`
	ConcurrencySafe      bool             `json:"concurrency_safe"`
	StartedAt            time.Time        `json:"started_at"`
	DurationMS           int64            `json:"duration_ms"`
	RevisionBefore       string           `json:"revision_before,omitempty"`
	RevisionAfter        string           `json:"revision_after,omitempty"`
	Success              bool             `json:"success"`
	Error                string           `json:"error,omitempty"`
	ErrorKind            string           `json:"error_kind,omitempty"`
	RawOutputBytes       int              `json:"raw_output_bytes"`
	ReturnedOutputBytes  int              `json:"returned_output_bytes"`
	ResultBudgeted       bool             `json:"result_budgeted"`
	ResultRef            string           `json:"result_ref,omitempty"`
	ArtifactRefs         []string         `json:"artifact_refs,omitempty"`
	ApprovalRef          string           `json:"approval_ref,omitempty"`
	PatchRiskSummary     *ToolPatchRisk   `json:"patch_risk_summary,omitempty"`
}

type ToolPatchRisk struct {
	FileCount      int            `json:"file_count"`
	HunkCount      int            `json:"hunk_count"`
	AddedLines     int            `json:"added_lines"`
	DeletedLines   int            `json:"deleted_lines"`
	Actions        map[string]int `json:"actions,omitempty"`
	MultiFile      bool           `json:"multi_file,omitempty"`
	ContainsDelete bool           `json:"contains_delete,omitempty"`
	ContainsMove   bool           `json:"contains_move,omitempty"`
	RiskLevel      string         `json:"risk_level"`
	ReviewHint     string         `json:"review_hint,omitempty"`
}

type toolApprovalRequest struct {
	ID                   string           `json:"id"`
	ToolName             string           `json:"tool_name"`
	CallID               string           `json:"call_id,omitempty"`
	Kind                 ToolKind         `json:"kind"`
	Risk                 ToolRisk         `json:"risk"`
	PolicyAction         ToolPolicyAction `json:"policy_action"`
	PolicyReason         string           `json:"policy_reason,omitempty"`
	ClassificationReason string           `json:"classification_reason,omitempty"`
	ReadOnly             bool             `json:"read_only"`
	Destructive          bool             `json:"destructive"`
	CreatedAt            time.Time        `json:"created_at"`
	Revision             string           `json:"revision,omitempty"`
	ArgumentsSHA256      string           `json:"arguments_sha256,omitempty"`
	ArgumentsPreview     string           `json:"arguments_preview,omitempty"`
	ModelNextAction      string           `json:"model_next_action"`
	ApprovalOptions      []string         `json:"approval_options"`
}

type toolTelemetry struct {
	mu      sync.RWMutex
	records []ToolExecutionRecord
}

func (t *toolTelemetry) record(record ToolExecutionRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = append(t.records, record)
}

func (t *toolTelemetry) snapshot() []ToolExecutionRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]ToolExecutionRecord, len(t.records))
	copy(out, t.records)
	return out
}

// ToolTelemetry returns a snapshot of tool execution records for this toolkit.
func (t *Toolkit) ToolTelemetry() []ToolExecutionRecord {
	return t.env.toolTelemetry.snapshot()
}

func (t *Toolkit) executeKnownTool(ctx context.Context, call providers.ToolCall, tool Tool) (string, error) {
	info := buildToolInfoForArgs(tool, t.toolExposure(call.Name), call.Arguments)
	decision := t.toolPolicy.Decide(info)
	startedAt := time.Now()
	revisionBefore := workspaceRevision(ctx, t.env.RootDir)

	if err := decision.blockingError(call.Name); err != nil {
		approvalRef := t.persistApprovalRequest(call, info, decision, startedAt, revisionBefore)
		t.recordToolExecution(call, info, decision, startedAt, revisionBefore, revisionBefore, "", "", "", false, approvalRef, err)
		return "", err
	}

	result, err := tool.Execute(ctx, call.Arguments)
	if info.Kind == ToolKindMCP {
		result = redactToolOutput(result)
	}
	returned := result
	resultRef := ""
	resultBudgeted := false
	if err == nil {
		returned, resultRef, resultBudgeted = MaybePersistResultWithRef(t.env.SessionDir, call.Name, call.ID, result, defaultResultBudget)
	}

	revisionAfter := workspaceRevision(ctx, t.env.RootDir)
	t.recordToolExecution(call, info, decision, startedAt, revisionBefore, revisionAfter, result, returned, resultRef, resultBudgeted, "", err)

	return returned, err
}

func (t *Toolkit) recordToolExecution(
	call providers.ToolCall,
	info ToolInfo,
	decision ToolPolicyDecision,
	startedAt time.Time,
	revisionBefore string,
	revisionAfter string,
	result string,
	returned string,
	resultRef string,
	resultBudgeted bool,
	approvalRef string,
	err error,
) {
	artifactRefs := extractToolArtifactRefs(result, resultRef)
	if approvalRef != "" {
		artifactRefs = appendUniqueString(artifactRefs, approvalRef)
	}
	record := ToolExecutionRecord{
		Name:                 call.Name,
		CallID:               call.ID,
		ArgumentsSHA256:      toolArgumentsSHA256(call.Arguments),
		Kind:                 info.Kind,
		Exposure:             info.Exposure,
		Risk:                 info.Risk,
		ClassificationReason: info.Reason,
		PolicyAction:         decision.Action,
		PolicyReason:         decision.Reason,
		ReadOnly:             info.ReadOnly,
		ConcurrencySafe:      info.ConcurrencySafe,
		StartedAt:            startedAt,
		DurationMS:           time.Since(startedAt).Milliseconds(),
		RevisionBefore:       revisionBefore,
		RevisionAfter:        revisionAfter,
		Success:              err == nil,
		RawOutputBytes:       len(result),
		ReturnedOutputBytes:  len(returned),
		ResultBudgeted:       resultBudgeted,
		ResultRef:            resultRef,
		ArtifactRefs:         artifactRefs,
		ApprovalRef:          approvalRef,
		PatchRiskSummary:     extractToolPatchRisk(call.Name, result),
	}
	if err != nil {
		record.Error = err.Error()
		record.ErrorKind = extractToolErrorKind(record.Error)
	}
	t.env.toolTelemetry.record(record)
}

func toolArgumentsSHA256(arguments string) string {
	sum := sha256.Sum256([]byte(arguments))
	return hex.EncodeToString(sum[:])
}

func (t *Toolkit) persistApprovalRequest(call providers.ToolCall, info ToolInfo, decision ToolPolicyDecision, createdAt time.Time, revision string) string {
	if t == nil || t.env == nil || strings.TrimSpace(t.env.SessionDir) == "" || decision.Action != ToolPolicyRequireApproval {
		return ""
	}
	id := approvalRequestID(call, createdAt)
	argsPreview := redactToolOutput(strings.TrimSpace(call.Arguments))
	if len(argsPreview) > 1200 {
		argsPreview = argsPreview[:1200] + "\n...[truncated]"
	}
	request := toolApprovalRequest{
		ID:                   id,
		ToolName:             call.Name,
		CallID:               call.ID,
		Kind:                 info.Kind,
		Risk:                 info.Risk,
		PolicyAction:         decision.Action,
		PolicyReason:         decision.Reason,
		ClassificationReason: info.Reason,
		ReadOnly:             info.ReadOnly,
		Destructive:          info.Destructive,
		CreatedAt:            createdAt.UTC(),
		Revision:             revision,
		ArgumentsSHA256:      toolArgumentsSHA256(call.Arguments),
		ArgumentsPreview:     argsPreview,
		ModelNextAction:      "ask the user for approval or choose a lower-risk alternative",
		ApprovalOptions:      []string{"ask_user", "choose_lower_risk_alternative", "stop"},
	}
	data, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return ""
	}
	dir := filepath.Join(t.env.SessionDir, "approvals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(dir, id+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return ""
	}
	return path
}

func approvalRequestID(call providers.ToolCall, createdAt time.Time) string {
	base := strings.TrimSpace(call.ID)
	if base == "" {
		base = fmt.Sprintf("%s-%d", strings.TrimSpace(call.Name), createdAt.UnixNano())
	}
	base = safeArtifactToken(base)
	if base == "" {
		base = fmt.Sprintf("approval-%d", createdAt.UnixNano())
	}
	return base
}

func safeArtifactToken(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-.")
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func extractToolErrorKind(message string) string {
	const marker = "error_kind="
	idx := strings.Index(message, marker)
	if idx < 0 {
		return ""
	}
	rest := message[idx+len(marker):]
	if rest == "" {
		return ""
	}
	if rest[0] == '"' || rest[0] == '\'' {
		quote := rest[0]
		rest = rest[1:]
		end := strings.IndexByte(rest, quote)
		if end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
	}
	end := 0
	for end < len(rest) {
		ch := rest[end]
		if (ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '_' || ch == '-' {
			end++
			continue
		}
		break
	}
	return strings.TrimSpace(rest[:end])
}

func extractToolArtifactRefs(result, resultRef string) []string {
	seen := map[string]bool{}
	out := []string(nil)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
	}
	add(resultRef)

	var payload any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return out
	}
	collectToolArtifactRefs(payload, add)
	return out
}

func extractToolPatchRisk(toolName, result string) *ToolPatchRisk {
	if toolName != "apply_patch" || strings.TrimSpace(result) == "" {
		return nil
	}
	var payload struct {
		RiskSummary ToolPatchRisk `json:"risk_summary"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil
	}
	if strings.TrimSpace(payload.RiskSummary.RiskLevel) == "" &&
		payload.RiskSummary.FileCount == 0 &&
		payload.RiskSummary.HunkCount == 0 {
		return nil
	}
	if payload.RiskSummary.Actions == nil {
		payload.RiskSummary.Actions = map[string]int{}
	}
	return &payload.RiskSummary
}

func collectToolArtifactRefs(value any, add func(string)) {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if toolArtifactRefKey(key) {
				collectArtifactValue(item, add)
				continue
			}
			collectToolArtifactRefs(item, add)
		}
	case []any:
		for _, item := range v {
			collectToolArtifactRefs(item, add)
		}
	}
}

func collectArtifactValue(value any, add func(string)) {
	switch v := value.(type) {
	case string:
		add(v)
	case []any:
		for _, item := range v {
			collectArtifactValue(item, add)
		}
	case map[string]any:
		if path, ok := v["path"].(string); ok {
			add(path)
		}
		if path, ok := v["report_path"].(string); ok {
			add(path)
		}
	}
}

func toolArtifactRefKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "full_log_ref", "result_ref", "artifact_ref", "artifact_refs",
		"artifact_path", "artifact_paths", "artifacts",
		"report_path", "final_report_path", "script_path",
		"trace_path", "patch_path", "manifest_path", "archive_path":
		return true
	default:
		return false
	}
}

func workspaceRevision(ctx context.Context, rootDir string) string {
	if rootDir == "" {
		return ""
	}
	revCtx, cancel := context.WithTimeout(ctx, workspaceRevisionTimeout)
	defer cancel()
	headCmd := exec.CommandContext(revCtx, "git", "rev-parse", "HEAD")
	headCmd.Dir = rootDir
	headOut, err := headCmd.Output()
	if err == nil {
		statusCmd := exec.CommandContext(revCtx, "git", "status", "--porcelain=v1", "-z")
		statusCmd.Dir = rootDir
		statusOut, err := statusCmd.Output()
		if err == nil {
			sum := sha256.Sum256(statusOut)
			head := string(headOut)
			if len(head) >= 12 {
				head = head[:12]
			}
			return "git:" + trimRevisionToken(head) + ":worktree:" + hex.EncodeToString(sum[:])[:16]
		}
	}

	digest, ok := filesystemWorkspaceRevision(revCtx, rootDir)
	if !ok {
		return ""
	}
	return "fs:worktree:" + digest[:16]
}

func filesystemWorkspaceRevision(ctx context.Context, rootDir string) (string, bool) {
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", false
	}

	hash := sha256.New()
	fileCount := 0
	var byteCount int64
	truncated := false

	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if isWorkspaceRevisionSkippedDir(name) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		mode := info.Mode()
		if mode.Type() != 0 {
			recordSpecialWorkspaceEntry(hash, rel, mode, path)
			return nil
		}
		if fileCount >= workspaceDigestMaxFiles || byteCount >= workspaceDigestMaxBytes {
			truncated = true
			return filepath.SkipAll
		}
		fileCount++
		fileDigest, copied, err := digestWorkspaceFile(path, workspaceDigestMaxBytesPerFile, workspaceDigestMaxBytes-byteCount)
		if err != nil {
			fmt.Fprintf(hash, "file\t%s\tunreadable\t%d\n", rel, info.Size())
			return nil
		}
		byteCount += copied
		if copied < info.Size() {
			truncated = true
		}
		fmt.Fprintf(hash, "file\t%s\t%d\t%x\n", rel, info.Size(), fileDigest)
		return nil
	})
	if err != nil && !errors.Is(err, filepath.SkipAll) {
		return "", false
	}
	fmt.Fprintf(hash, "summary\tfiles=%d\tbytes=%d\ttruncated=%t\n", fileCount, byteCount, truncated)
	return hex.EncodeToString(hash.Sum(nil)), true
}

func isWorkspaceRevisionSkippedDir(name string) bool {
	switch name {
	case ".wuu-home":
		return true
	default:
		return isSkippedDir(name)
	}
}

func recordSpecialWorkspaceEntry(hash io.Writer, rel string, mode os.FileMode, path string) {
	if mode&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err == nil {
			fmt.Fprintf(hash, "symlink\t%s\t%s\n", rel, target)
			return
		}
	}
	fmt.Fprintf(hash, "special\t%s\t%s\n", rel, mode.Type().String())
}

func digestWorkspaceFile(path string, perFileLimit, remainingLimit int64) ([]byte, int64, error) {
	if remainingLimit <= 0 {
		return nil, 0, nil
	}
	limit := perFileLimit
	if remainingLimit < limit {
		limit = remainingLimit
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	hash := sha256.New()
	copied, err := io.CopyN(hash, file, limit)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, copied, err
	}
	return hash.Sum(nil), copied, nil
}

func trimRevisionToken(value string) string {
	out := make([]byte, 0, len(value))
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= 'a' && c <= 'f':
			out = append(out, c)
		case c >= 'A' && c <= 'F':
			out = append(out, c+'a'-'A')
		case c >= '0' && c <= '9':
			out = append(out, c)
		}
	}
	return string(out)
}
