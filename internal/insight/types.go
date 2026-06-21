package insight

import (
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// SessionMeta is extracted from a session JSONL file without LLM calls.
type SessionMeta struct {
	ID                  string         `json:"id"`
	CreatedAt           time.Time      `json:"created_at"`
	Duration            time.Duration  `json:"duration"`
	UserMessages        int            `json:"user_messages"`
	AssistantMsgs       int            `json:"assistant_messages"`
	ToolCounts          map[string]int `json:"tool_counts"`
	Languages           map[string]int `json:"languages"`
	EstTokens           int            `json:"est_tokens"`
	InputTokens         int            `json:"input_tokens"`
	OutputTokens        int            `json:"output_tokens"`
	CacheCreationTokens int            `json:"cache_creation_tokens"`
	CacheReadTokens     int            `json:"cache_read_tokens"`
	FirstUserMsg        string         `json:"first_user_msg"`
	MessageHours        []int          `json:"message_hours"`
	LinesAdded          int            `json:"lines_added"`
	LinesRemoved        int            `json:"lines_removed"`
	FilesModified       int            `json:"files_modified"`
	UserTimestamps      []time.Time    `json:"user_timestamps"`
	// ModelBreakdowns maps "provider|model" keys to per-bucket usage within
	// this session. The key is an internal identifier; UI code renders
	// empty provider+model as "(unknown)". Populated only when the session
	// has at least one token_usage meta row with usable data.
	ModelBreakdowns map[string]*ModelUsage `json:"model_breakdowns"`
}

// ModelUsage aggregates token consumption and session count for one
// provider/model pair. Empty Provider+Model represents legacy token_usage
// rows persisted before provider/model were tracked; UI code renders
// these as "(unknown)".
type ModelUsage struct {
	Provider            string `json:"provider"`
	Model               string `json:"model"`
	InputTokens         int    `json:"input_tokens"`
	OutputTokens        int    `json:"output_tokens"`
	CacheCreationTokens int    `json:"cache_creation_tokens"`
	CacheReadTokens     int    `json:"cache_read_tokens"`
	Sessions            int    `json:"sessions"`
}

// TotalContextTokens mirrors providers.TokenUsage.TotalContextTokens.
func (m ModelUsage) TotalContextTokens() int {
	return m.InputTokens + m.CacheReadTokens + m.OutputTokens
}

// CacheHitRate returns the prompt-cache hit rate across this bucket.
// Returns nil when there is no input to cache.
func (m ModelUsage) CacheHitRate() *float64 {
	promptTokens := m.InputTokens + m.CacheReadTokens
	if promptTokens <= 0 {
		return nil
	}
	rate := float64(m.CacheReadTokens) / float64(promptTokens)
	return &rate
}

// Facet is the LLM-extracted structured analysis of a single session.
type Facet struct {
	SessionID      string         `json:"session_id"`
	Goal           string         `json:"goal"`
	GoalCategories map[string]int `json:"goal_categories"`
	Outcome        string         `json:"outcome"`
	Satisfaction   string         `json:"satisfaction"`
	Helpfulness    string         `json:"helpfulness"`
	SessionType    string         `json:"session_type"`
	Friction       map[string]int `json:"friction"`
	FrictionDetail string         `json:"friction_detail"`
	PrimarySuccess string         `json:"primary_success"`
	Summary        string         `json:"summary"`
	ExtractedAt    int64          `json:"extracted_at"`
}

// TokenUsageRow is one token_usage meta row extracted from a session
// history. It carries the per-row timestamp so callers can bucket usage
// by day or pick the most recent N rows for a "最近记录" list, neither
// of which can be done from the pre-aggregated SessionMeta alone.
// At is taken from the persisted record (UTC); rows with a zero At are
// always included for "all" range but excluded from any time-windowed
// query so a malformed legacy record cannot be pinned to "today".
type TokenUsageRow struct {
	SessionID          string
	At                 time.Time
	Provider           string
	Model              string
	InputTokens        int
	OutputTokens       int
	CacheCreationTokens int
	CacheReadTokens    int
}

// AggregatedData combines statistics from all sessions and facets.
type AggregatedData struct {
	TotalSessions            int              `json:"total_sessions"`
	SessionsWithFacets       int              `json:"sessions_with_facets"`
	DateRange                [2]string        `json:"date_range"`
	TotalMessages            int              `json:"total_messages"`
	TotalDurationH           float64          `json:"total_duration_hours"`
	TotalEstTokens           int              `json:"total_est_tokens"`
	TotalInputTokens         int              `json:"total_input_tokens"`
	TotalOutputTokens        int              `json:"total_output_tokens"`
	TotalCacheCreationTokens int              `json:"total_cache_creation_tokens"`
	TotalCacheReadTokens     int              `json:"total_cache_read_tokens"`
	ToolCounts               map[string]int   `json:"tool_counts"`
	Languages                map[string]int   `json:"languages"`
	GoalCategories           map[string]int   `json:"goal_categories"`
	Outcomes                 map[string]int   `json:"outcomes"`
	Satisfaction             map[string]int   `json:"satisfaction"`
	SessionTypes             map[string]int   `json:"session_types"`
	Friction                 map[string]int   `json:"friction"`
	Success                  map[string]int   `json:"success"`
	Summaries                []SessionSummary `json:"summaries"`
	TotalLinesAdded          int              `json:"total_lines_added"`
	TotalLinesRemoved        int              `json:"total_lines_removed"`
	TotalFilesModified       int              `json:"total_files_modified"`
	DaysActive               int              `json:"days_active"`
	MessagesPerDay           float64          `json:"messages_per_day"`
	MessageHours             []int            `json:"message_hours"`
	// ModelBreakdowns aggregates per provider/model across all sessions,
	// sorted by total context tokens descending. Empty provider+model
	// buckets (legacy rows) appear last with display label "(unknown)".
	ModelBreakdowns []ModelUsage `json:"model_breakdowns"`
	// OverallCacheHitRate is the prompt-cache hit rate across all usage
	// (weighted by token count). Zero when no usage data exists.
	OverallCacheHitRate float64 `json:"overall_cache_hit_rate"`
}

// SessionSummary is a short summary entry used in aggregated data.
type SessionSummary struct {
	ID      string `json:"id"`
	Date    string `json:"date"`
	Summary string `json:"summary"`
	Goal    string `json:"goal,omitempty"`
}

// InsightSection is one section of the generated report.
type InsightSection struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

// AtAGlance is the four-part summary synthesized from all sections.
type AtAGlance struct {
	WhatsWorking       string `json:"whats_working"`
	WhatsHindering     string `json:"whats_hindering"`
	QuickWins          string `json:"quick_wins"`
	AmbitiousWorkflows string `json:"ambitious_workflows"`
}

// Report is the final assembled insight output.
type Report struct {
	AtAGlance   AtAGlance        `json:"at_a_glance"`
	Sections    []InsightSection `json:"sections"`
	Stats       AggregatedData   `json:"stats"`
	GeneratedAt time.Time        `json:"generated_at"`
	HTMLPath    string           `json:"-"` // path to generated HTML report file
}

// ProgressEvent is sent from the insight goroutine to interested clients.
type ProgressEvent struct {
	Phase  string  // "scanning", "extracting", "generating", "synthesizing", "done", "error"
	Detail string  // human-readable progress message
	Pct    float64 // 0.0–1.0
	Report *Report // non-nil only when Phase == "done"
	Err    error   // non-nil only when Phase == "error"
}

// RunConfig holds all dependencies for an insight run.
type RunConfig struct {
	SessionDir    string
	WorkspaceRoot string
	OutputDir     string
	ScopeCWD      string
	Client        providers.Client
	Model         string
	MaxSessions   int
}
