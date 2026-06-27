package sessiontrace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/modelbudget"
	"github.com/blueberrycongee/wuu/internal/modelprofile"
	"github.com/blueberrycongee/wuu/internal/stringutil"
	"github.com/blueberrycongee/wuu/internal/tools"
)

const finalAnswerPreviewBytes = 1200

var secretLikeRE = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|auth[_-]?token|bearer|password|secret)\s*[:= ]+\s*[^ \t\r\n]+`)

type Event struct {
	Type      string    `json:"type"`
	ThreadID  string    `json:"thread_id,omitempty"`
	TurnID    string    `json:"turn_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Data      any       `json:"data,omitempty"`
}

type TurnRecord struct {
	ThreadID            string              `json:"thread_id"`
	TurnID              string              `json:"turn_id"`
	Status              string              `json:"status"`
	ProviderName        string              `json:"provider_name,omitempty"`
	Model               string              `json:"model,omitempty"`
	APIModel            string              `json:"api_model,omitempty"`
	ModelProfile        *ModelProfileRecord `json:"model_profile,omitempty"`
	StartedAt           *time.Time          `json:"started_at,omitempty"`
	CompletedAt         *time.Time          `json:"completed_at,omitempty"`
	DurationMS          *int64              `json:"duration_ms,omitempty"`
	InputTokens         int                 `json:"input_tokens,omitempty"`
	OutputTokens        int                 `json:"output_tokens,omitempty"`
	CacheCreationTokens int                 `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens     int                 `json:"cache_read_tokens,omitempty"`
	FinishReason        string              `json:"finish_reason,omitempty"`
	StopReason          string              `json:"stop_reason,omitempty"`
	Truncated           bool                `json:"truncated,omitempty"`
	HistoryRewritten    bool                `json:"history_rewritten,omitempty"`
	Error               string              `json:"error,omitempty"`
}

type ModelProfileRecord struct {
	ProviderName              string `json:"provider_name,omitempty"`
	Model                     string `json:"model,omitempty"`
	APIModel                  string `json:"api_model,omitempty"`
	Family                    string `json:"family,omitempty"`
	ToolCalling               string `json:"tool_calling,omitempty"`
	FreeformTool              bool   `json:"freeform_tool,omitempty"`
	ParallelToolCalls         bool   `json:"parallel_tool_calls,omitempty"`
	ContextWindowTokens       int    `json:"context_window_tokens,omitempty"`
	ContextWindowSource       string `json:"context_window_source,omitempty"`
	ContextWindowKnown        bool   `json:"context_window_known,omitempty"`
	InputLimitTokens          int    `json:"input_limit_tokens,omitempty"`
	OutputLimitTokens         int    `json:"output_limit_tokens,omitempty"`
	OutputReserveTokens       int    `json:"output_reserve_tokens,omitempty"`
	UsableInputTokens         int    `json:"usable_input_tokens,omitempty"`
	CompactThresholdTokens    int    `json:"compact_threshold_tokens,omitempty"`
	DefaultWriteMode          string `json:"default_write_mode,omitempty"`
	DefaultSearchBudget       int    `json:"default_search_budget,omitempty"`
	DefaultMaxAutonomousSteps int    `json:"default_max_autonomous_steps,omitempty"`
	NeedsReadBeforeWrite      bool   `json:"needs_read_before_write,omitempty"`
	AllowParallelReadOnly     bool   `json:"allow_parallel_read_only,omitempty"`
	AllowDirectShell          bool   `json:"allow_direct_shell,omitempty"`
}

func NewModelProfileRecord(providerName, model, apiModel string) *ModelProfileRecord {
	return NewModelProfileRecordWithBudget(providerName, model, apiModel, modelbudget.Budget{})
}

func NewModelProfileRecordWithBudget(providerName, model, apiModel string, budget modelbudget.Budget) *ModelProfileRecord {
	providerName = strings.TrimSpace(providerName)
	model = strings.TrimSpace(model)
	apiModel = strings.TrimSpace(apiModel)
	if providerName == "" && model == "" && apiModel == "" {
		return nil
	}
	modelForProfile := apiModel
	if modelForProfile == "" {
		modelForProfile = model
	}
	profile := modelprofile.Resolve(providerName, modelForProfile)
	contextWindow := profile.Context.WindowTokens
	contextSource := ""
	contextKnown := false
	if budget.ContextWindowSource != "" {
		contextWindow = budget.ContextWindowTokens
		contextSource = string(budget.ContextWindowSource)
		contextKnown = budget.ContextWindowKnown
	}
	return &ModelProfileRecord{
		ProviderName:              providerName,
		Model:                     model,
		APIModel:                  apiModel,
		Family:                    string(profile.Family),
		ToolCalling:               string(profile.APIShape.ToolCalling),
		FreeformTool:              profile.APIShape.FreeformTool,
		ParallelToolCalls:         profile.APIShape.ParallelToolCalls,
		ContextWindowTokens:       contextWindow,
		ContextWindowSource:       contextSource,
		ContextWindowKnown:        contextKnown,
		InputLimitTokens:          budget.InputLimitTokens,
		OutputLimitTokens:         budget.OutputLimitTokens,
		OutputReserveTokens:       budget.OutputReserveTokens,
		UsableInputTokens:         budget.UsableInputTokens,
		CompactThresholdTokens:    budget.CompactThresholdTokens,
		DefaultWriteMode:          string(profile.Workflow.DefaultWriteMode),
		DefaultSearchBudget:       profile.Workflow.DefaultSearchBudget,
		DefaultMaxAutonomousSteps: profile.Workflow.DefaultMaxAutonomousSteps,
		NeedsReadBeforeWrite:      profile.Workflow.NeedsReadBeforeWrite,
		AllowParallelReadOnly:     profile.Workflow.AllowParallelReadOnly,
		AllowDirectShell:          profile.Workflow.AllowDirectShell,
	}
}

type FinalRecord struct {
	Status              string `json:"status"`
	InputTokens         int    `json:"input_tokens,omitempty"`
	OutputTokens        int    `json:"output_tokens,omitempty"`
	CacheCreationTokens int    `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens     int    `json:"cache_read_tokens,omitempty"`
	FinishReason        string `json:"finish_reason,omitempty"`
	StopReason          string `json:"stop_reason,omitempty"`
	Truncated           bool   `json:"truncated,omitempty"`
	FinalAnswerPreview  string `json:"final_answer_preview,omitempty"`
	Error               string `json:"error,omitempty"`
}

type ReplaySummary struct {
	Path              string                 `json:"path,omitempty"`
	Mode              string                 `json:"mode"`
	EventCount        int                    `json:"event_count"`
	EventTypes        map[string]int         `json:"event_types,omitempty"`
	Turns             []TurnRecord           `json:"turns,omitempty"`
	LatestTurn        *TurnRecord            `json:"latest_turn,omitempty"`
	ContextRequests   []RequestContextRecord `json:"context_requests,omitempty"`
	ProviderStates    []ProviderStateRecord  `json:"provider_states,omitempty"`
	ContextBlockKinds []string               `json:"context_block_kinds,omitempty"`
	ToolInventory     []tools.ToolInfo       `json:"tool_inventory,omitempty"`
	ToolNames         []string               `json:"tool_names,omitempty"`
	ToolSummary       *ToolSummary           `json:"tool_summary,omitempty"`
	Final             *FinalRecord           `json:"final,omitempty"`
	Complete          bool                   `json:"complete"`
	Warnings          []string               `json:"warnings,omitempty"`
}

type RequestContextRecord struct {
	StepIndex                int                   `json:"step_index"`
	TransientMessages        int                   `json:"transient_messages,omitempty"`
	ContentBytes             int                   `json:"content_bytes,omitempty"`
	BlockKinds               []string              `json:"block_kinds,omitempty"`
	BlockKindCounts          map[string]int        `json:"block_kind_counts,omitempty"`
	BlockKindBytes           map[string]int        `json:"block_kind_bytes,omitempty"`
	SegmentLifecycleCounts   map[string]int        `json:"segment_lifecycle_counts,omitempty"`
	SegmentPlacementCounts   map[string]int        `json:"segment_placement_counts,omitempty"`
	SegmentCachePolicyCounts map[string]int        `json:"segment_cache_policy_counts,omitempty"`
	MessageCount             int                   `json:"message_count,omitempty"`
	SystemMessages           int                   `json:"system_messages,omitempty"`
	HiddenMessages           int                   `json:"hidden_messages,omitempty"`
	ToolCount                int                   `json:"tool_count,omitempty"`
	StablePrefix             int                   `json:"stable_prefix,omitempty"`
	TurnPrefix               int                   `json:"turn_prefix,omitempty"`
	DynamicBytes             int                   `json:"dynamic_context_bytes,omitempty"`
	SystemBytes              int                   `json:"system_bytes,omitempty"`
	StablePrefixBytes        int                   `json:"stable_prefix_bytes,omitempty"`
	TurnPrefixBytes          int                   `json:"turn_prefix_bytes,omitempty"`
	MessageBytes             int                   `json:"message_bytes,omitempty"`
	ToolSchemaBytes          int                   `json:"tool_schema_bytes,omitempty"`
	LoadableToolCount        int                   `json:"loadable_tool_count,omitempty"`
	LoadableToolSchemaBytes  int                   `json:"loadable_tool_schema_bytes,omitempty"`
	LoadableToolSurfaceHash  string                `json:"loadable_tool_surface_hash,omitempty"`
	SystemHash               string                `json:"system_hash,omitempty"`
	StablePrefixHash         string                `json:"stable_prefix_hash,omitempty"`
	TurnPrefixHash           string                `json:"turn_prefix_hash,omitempty"`
	ToolSurfaceHash          string                `json:"tool_surface_hash,omitempty"`
	PromptCacheKey           string                `json:"prompt_cache_key,omitempty"`
	SystemSections           []SystemSectionRecord `json:"system_sections,omitempty"`
}

type SystemSectionRecord struct {
	Key    string `json:"key"`
	Static bool   `json:"static"`
	Bytes  int    `json:"bytes"`
	Hash   string `json:"hash,omitempty"`
}

type ProviderStateRecord struct {
	Provider               string `json:"provider,omitempty"`
	Protocol               string `json:"protocol,omitempty"`
	ReplayMode             string `json:"replay_mode,omitempty"`
	PreviousResponseIDUsed bool   `json:"previous_response_id_used,omitempty"`
	InputItems             int    `json:"input_items,omitempty"`
	FullInputItems         int    `json:"full_input_items,omitempty"`
	DeltaInputItems        int    `json:"delta_input_items,omitempty"`
}

type ToolSummary struct {
	Total             int                           `json:"total"`
	Succeeded         int                           `json:"succeeded"`
	Failed            int                           `json:"failed"`
	ByKind            map[string]int                `json:"by_kind,omitempty"`
	ByRisk            map[string]int                `json:"by_risk,omitempty"`
	ByPolicyAction    map[string]int                `json:"by_policy_action,omitempty"`
	ByResultAction    map[string]int                `json:"by_result_action,omitempty"`
	ByErrorKind       map[string]int                `json:"by_error_kind,omitempty"`
	PolicyBlocks      []ToolPolicyBlockSummary      `json:"policy_blocks,omitempty"`
	RepeatedArguments []ToolRepeatedArgumentSummary `json:"repeated_arguments,omitempty"`
	argumentCounts    map[string]ToolRepeatedArgumentSummary
}

type ToolPolicyBlockSummary struct {
	ToolName        string `json:"tool_name"`
	CallID          string `json:"call_id,omitempty"`
	PolicyAction    string `json:"policy_action"`
	PolicyReason    string `json:"policy_reason,omitempty"`
	Risk            string `json:"risk,omitempty"`
	ErrorKind       string `json:"error_kind,omitempty"`
	ArgumentsSHA256 string `json:"arguments_sha256,omitempty"`
	ApprovalRef     string `json:"approval_ref,omitempty"`
	RevisionBefore  string `json:"revision_before,omitempty"`
	ModelNextAction string `json:"model_next_action,omitempty"`
}

type ToolRepeatedArgumentSummary struct {
	ToolName        string `json:"tool_name"`
	ArgumentsSHA256 string `json:"arguments_sha256"`
	Count           int    `json:"count"`
}

func Path(sessionDir string) string {
	sessionDir = strings.TrimSpace(sessionDir)
	if sessionDir == "" {
		return ""
	}
	return filepath.Join(sessionDir, "session-trace.jsonl")
}

func ReplayTrace(path string) (ReplaySummary, error) {
	file, err := os.Open(path)
	if err != nil {
		return ReplaySummary{}, err
	}
	defer file.Close()

	summary := ReplaySummary{
		Path:       path,
		Mode:       "session_trace_replay",
		EventTypes: make(map[string]int),
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		var event struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data,omitempty"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return ReplaySummary{}, fmt.Errorf("decode trace line %d: %w", line, err)
		}
		summary.EventCount++
		summary.EventTypes[event.Type]++
		if err := replayEvent(&summary, event.Type, event.Data); err != nil {
			return ReplaySummary{}, fmt.Errorf("replay trace line %d type %q: %w", line, event.Type, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return ReplaySummary{}, err
	}
	if len(summary.Turns) == 0 {
		return ReplaySummary{}, fmt.Errorf("trace %s has no turn event", path)
	}
	if summary.Final == nil {
		summary.Warnings = append(summary.Warnings, "trace has no final event")
	}
	summary.finalizeToolSummary()
	summary.Complete = summary.EventTypes["turn"] > 0 && summary.EventTypes["final"] > 0
	return summary, nil
}

func replayEvent(summary *ReplaySummary, eventType string, data json.RawMessage) error {
	if len(data) == 0 {
		return nil
	}
	switch eventType {
	case "turn":
		var turn TurnRecord
		if err := json.Unmarshal(data, &turn); err != nil {
			return err
		}
		summary.Turns = append(summary.Turns, turn)
		summary.LatestTurn = &summary.Turns[len(summary.Turns)-1]
	case "context_requests":
		var records []RequestContextRecord
		if err := json.Unmarshal(data, &records); err != nil {
			return err
		}
		summary.ContextRequests = append(summary.ContextRequests, records...)
		summary.addContextBlockKinds(records)
	case "provider_states":
		var records []ProviderStateRecord
		if err := json.Unmarshal(data, &records); err != nil {
			return err
		}
		summary.ProviderStates = append(summary.ProviderStates, records...)
	case "tool_inventory":
		var inventory []tools.ToolInfo
		if err := json.Unmarshal(data, &inventory); err != nil {
			return err
		}
		summary.ToolInventory = inventory
	case "tool_records":
		var records []tools.ToolExecutionRecord
		if err := json.Unmarshal(data, &records); err != nil {
			return err
		}
		for _, record := range records {
			if strings.TrimSpace(record.Name) != "" {
				summary.ToolNames = append(summary.ToolNames, record.Name)
			}
			summary.addToolRecord(record)
		}
	case "final":
		var final FinalRecord
		if err := json.Unmarshal(data, &final); err != nil {
			return err
		}
		summary.Final = &final
	}
	return nil
}

func (summary *ReplaySummary) addContextBlockKinds(records []RequestContextRecord) {
	seen := make(map[string]bool, len(summary.ContextBlockKinds))
	for _, kind := range summary.ContextBlockKinds {
		seen[kind] = true
	}
	for _, record := range records {
		for _, kind := range record.BlockKinds {
			kind = strings.TrimSpace(kind)
			if kind == "" || seen[kind] {
				continue
			}
			seen[kind] = true
			summary.ContextBlockKinds = append(summary.ContextBlockKinds, kind)
		}
	}
}

func (summary *ReplaySummary) addToolRecord(record tools.ToolExecutionRecord) {
	if summary.ToolSummary == nil {
		summary.ToolSummary = &ToolSummary{
			ByKind:         map[string]int{},
			ByRisk:         map[string]int{},
			ByPolicyAction: map[string]int{},
			ByResultAction: map[string]int{},
			ByErrorKind:    map[string]int{},
		}
	}
	summary.ToolSummary.Total++
	if record.Success {
		summary.ToolSummary.Succeeded++
	} else {
		summary.ToolSummary.Failed++
	}
	if kind := strings.TrimSpace(string(record.Kind)); kind != "" {
		summary.ToolSummary.ByKind[kind]++
	}
	if risk := strings.TrimSpace(string(record.Risk)); risk != "" {
		summary.ToolSummary.ByRisk[risk]++
	}
	if action := strings.TrimSpace(string(record.PolicyAction)); action != "" {
		summary.ToolSummary.ByPolicyAction[action]++
	}
	if resultAction := toolResultActionKey(record.Name, record.ResultAction); resultAction != "" {
		summary.ToolSummary.ByResultAction[resultAction]++
	}
	if errorKind := strings.TrimSpace(record.ErrorKind); errorKind != "" {
		summary.ToolSummary.ByErrorKind[errorKind]++
	}
	summary.addToolPolicyBlock(record)
	summary.addRepeatedToolArguments(record.Name, record.ArgumentsSHA256)
}

func toolResultActionKey(toolName, action string) string {
	toolName = strings.TrimSpace(toolName)
	action = strings.TrimSpace(action)
	if toolName == "" || action == "" {
		return ""
	}
	return toolName + ":" + action
}

func (summary *ReplaySummary) addToolPolicyBlock(record tools.ToolExecutionRecord) {
	action := strings.TrimSpace(string(record.PolicyAction))
	errorKind := strings.TrimSpace(record.ErrorKind)
	autoModeBlocked := record.PolicyAction == tools.ToolPolicyAutoClassify &&
		(errorKind == "auto_mode_denied" ||
			errorKind == "auto_classifier_unavailable" ||
			errorKind == "auto_classifier_error")
	if record.PolicyAction != tools.ToolPolicyDeny &&
		record.PolicyAction != tools.ToolPolicyRequireApproval &&
		!autoModeBlocked &&
		errorKind != "policy_denied" &&
		errorKind != "approval_required" &&
		errorKind != "auto_mode_denied" &&
		errorKind != "auto_classifier_unavailable" &&
		errorKind != "auto_classifier_error" {
		return
	}
	toolName := strings.TrimSpace(record.Name)
	if toolName == "" {
		return
	}
	if summary.ToolSummary == nil {
		summary.ToolSummary = &ToolSummary{}
	}
	summary.ToolSummary.PolicyBlocks = append(summary.ToolSummary.PolicyBlocks, ToolPolicyBlockSummary{
		ToolName:        toolName,
		CallID:          strings.TrimSpace(record.CallID),
		PolicyAction:    action,
		PolicyReason:    strings.TrimSpace(record.PolicyReason),
		Risk:            strings.TrimSpace(string(record.Risk)),
		ErrorKind:       errorKind,
		ArgumentsSHA256: strings.TrimSpace(record.ArgumentsSHA256),
		ApprovalRef:     strings.TrimSpace(record.ApprovalRef),
		RevisionBefore:  strings.TrimSpace(record.RevisionBefore),
		ModelNextAction: modelNextActionForPolicyBlock(record.PolicyAction, errorKind),
	})
}

func modelNextActionForPolicyBlock(action tools.ToolPolicyAction, errorKind string) string {
	switch {
	case action == tools.ToolPolicyRequireApproval || errorKind == "approval_required":
		return "ask the user for approval or choose a lower-risk alternative"
	case action == tools.ToolPolicyAutoClassify || errorKind == "auto_mode_denied" || errorKind == "auto_classifier_unavailable" || errorKind == "auto_classifier_error":
		return "ask the user for approval or choose a lower-risk alternative"
	case action == tools.ToolPolicyDeny || errorKind == "policy_denied":
		return "choose a lower-risk tool or explain that policy blocks the requested action"
	default:
		return ""
	}
}

func (summary *ReplaySummary) addRepeatedToolArguments(toolName, argumentsSHA256 string) {
	toolName = strings.TrimSpace(toolName)
	argumentsSHA256 = strings.TrimSpace(argumentsSHA256)
	if toolName == "" || argumentsSHA256 == "" {
		return
	}
	if summary.ToolSummary == nil {
		summary.ToolSummary = &ToolSummary{}
	}
	if summary.ToolSummary.argumentCounts == nil {
		summary.ToolSummary.argumentCounts = map[string]ToolRepeatedArgumentSummary{}
	}
	key := toolName + "\x00" + argumentsSHA256
	entry := summary.ToolSummary.argumentCounts[key]
	if entry.Count == 0 {
		entry.ToolName = toolName
		entry.ArgumentsSHA256 = argumentsSHA256
	}
	entry.Count++
	summary.ToolSummary.argumentCounts[key] = entry
}

func (summary *ReplaySummary) finalizeToolSummary() {
	if summary.ToolSummary == nil || len(summary.ToolSummary.argumentCounts) == 0 {
		return
	}
	summary.ToolSummary.RepeatedArguments = summary.ToolSummary.RepeatedArguments[:0]
	for _, entry := range summary.ToolSummary.argumentCounts {
		if entry.Count > 1 {
			summary.ToolSummary.RepeatedArguments = append(summary.ToolSummary.RepeatedArguments, entry)
		}
	}
	sort.Slice(summary.ToolSummary.RepeatedArguments, func(i, j int) bool {
		left := summary.ToolSummary.RepeatedArguments[i]
		right := summary.ToolSummary.RepeatedArguments[j]
		if left.ToolName != right.ToolName {
			return left.ToolName < right.ToolName
		}
		return left.ArgumentsSHA256 < right.ArgumentsSHA256
	})
	summary.ToolSummary.argumentCounts = nil
}

func AppendTurn(path string, turn TurnRecord, final FinalRecord, inventory []tools.ToolInfo, records []tools.ToolExecutionRecord, contextRequests []RequestContextRecord, providerStates []ProviderStateRecord) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	createdAt := time.Now().UTC()
	encoder := json.NewEncoder(file)
	events := []Event{{
		Type:      "turn",
		ThreadID:  turn.ThreadID,
		TurnID:    turn.TurnID,
		CreatedAt: createdAt,
		Data:      turn,
	}}
	if len(contextRequests) > 0 {
		events = append(events, Event{
			Type:      "context_requests",
			ThreadID:  turn.ThreadID,
			TurnID:    turn.TurnID,
			CreatedAt: createdAt,
			Data:      append([]RequestContextRecord(nil), contextRequests...),
		})
	}
	if len(providerStates) > 0 {
		events = append(events, Event{
			Type:      "provider_states",
			ThreadID:  turn.ThreadID,
			TurnID:    turn.TurnID,
			CreatedAt: createdAt,
			Data:      append([]ProviderStateRecord(nil), providerStates...),
		})
	}
	if len(inventory) > 0 {
		events = append(events, Event{
			Type:      "tool_inventory",
			ThreadID:  turn.ThreadID,
			TurnID:    turn.TurnID,
			CreatedAt: createdAt,
			Data:      append([]tools.ToolInfo(nil), inventory...),
		})
	}
	if len(records) > 0 {
		events = append(events, Event{
			Type:      "tool_records",
			ThreadID:  turn.ThreadID,
			TurnID:    turn.TurnID,
			CreatedAt: createdAt,
			Data:      append([]tools.ToolExecutionRecord(nil), records...),
		})
	}
	final.FinalAnswerPreview = redactAndTruncate(final.FinalAnswerPreview)
	final.Error = redactAndTruncate(final.Error)
	events = append(events, Event{
		Type:      "final",
		ThreadID:  turn.ThreadID,
		TurnID:    turn.TurnID,
		CreatedAt: createdAt,
		Data:      final,
	})
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}
	return nil
}

func redactAndTruncate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = secretLikeRE.ReplaceAllString(value, "$1=[REDACTED]")
	if len(value) <= finalAnswerPreviewBytes {
		return value
	}
	return stringutil.Truncate(value, finalAnswerPreviewBytes, "...[truncated]")
}
