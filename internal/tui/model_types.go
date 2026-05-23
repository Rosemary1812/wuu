package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/cron"
	"github.com/blueberrycongee/wuu/internal/eventbus"
	"github.com/blueberrycongee/wuu/internal/hooks"
	"github.com/blueberrycongee/wuu/internal/insight"
	"github.com/blueberrycongee/wuu/internal/markdown"
	"github.com/blueberrycongee/wuu/internal/memory"
	processruntime "github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/skills"
	"github.com/blueberrycongee/wuu/internal/subagent"
	"github.com/blueberrycongee/wuu/internal/tools"
)

const (
	minOutputHeight = 6
	// interactiveStreamDrainLimit caps how many already-queued stream
	// events we opportunistically apply during non-stream UI work
	// (mouse drag/select, spinner ticks). Without this side-drain, a
	// burst of mouse motion can starve the single waitStreamEvent
	// command long enough for live reply rendering to look "stuck".
	interactiveStreamDrainLimit = 8

	queuePreviewMaxItems = 2
	queuePreviewMaxChars = 28

	chatSelectionDragThreshold = 1

	// maxAutoResumeChain caps how many turns the main agent can
	// auto-fire in response to worker completions without seeing
	// fresh user input. A pure safety net — modern models stop
	// naturally well before this in normal use.
	maxAutoResumeChain = 100
)

var defaultInputTextarea = newInputTextarea()

type tickMsg struct {
	now time.Time
}

type streamEventMsg struct {
	event providers.StreamEvent
}

type streamFinishedMsg struct{}
type ctrlCResetMsg struct{}

type queueDrainMsg struct{}

type cronFireMsg struct {
	task cron.Task
}

type inlineSpinMsg struct{}
type processPollMsg struct{}
type processNotifyMsg struct {
	event processruntime.Event
}

// selectionAutoScrollMsg drives the recurring viewport scroll while a
// drag-select is held past the chat area's edge. seq must match the
// model's current selectionAutoScroll.seq for the tick to take effect;
// stale ticks (from a burst the user has already left) self-discard.
type selectionAutoScrollMsg struct {
	seq int
}

// selectionAutoScrollState captures everything needed to keep
// scrolling without further mouse motion events. dir is -1 (up) or
// +1 (down). speed is the number of content lines to advance per
// tick — proportional to how far past the edge the cursor sat at
// the moment we (re)started ticking, so dragging further past the
// edge scrolls faster, mirroring most desktop editors. lastX is the
// most recent screen X so we can re-derive the selection focus
// column on every tick (the user's mouse hasn't moved, but the
// content under it has). seq is bumped on every (de)activation so
// in-flight ticks from a previous burst exit cleanly.
type selectionAutoScrollState struct {
	active bool
	dir    int
	speed  int
	lastX  int
	seq    int
}

type insightProgressMsg struct {
	event insight.ProgressEvent
}

type insightFinishedMsg struct{}

// workerNotifyMsg is delivered when a sub-agent's status changes.
type workerNotifyMsg struct {
	notification subagent.Notification
}

type ToolCallStatus string

const (
	ToolCallRunning ToolCallStatus = "running"
	ToolCallDone    ToolCallStatus = "done"
	ToolCallError   ToolCallStatus = "error"
)

type ToolCallEntry struct {
	ID        string
	Name      string
	Args      string
	Result    string
	Status    ToolCallStatus
	Collapsed bool

	// cachedCard is the fully rendered tool card string. Invalidated
	// when Status, Result, Collapsed, or Args changes. Avoids
	// re-parsing JSON and re-rendering on every viewport refresh.
	cachedCard      string
	cachedCardKey   string // "status:collapsed:argsLen:resultLen"
	cachedCardWidth int
}

type transcriptEntry struct {
	Role          string
	Content       string   // raw content
	rendered      string   // markdown-rendered text (cached)
	renderedLines []string // lines accumulated from StreamCollector commits
	renderStart   int      // inclusive content line in the last rendered viewport snapshot
	renderEnd     int      // inclusive content line in the last rendered viewport snapshot

	// composited is the fully rendered entry output including tool
	// cards, thinking blocks, content, indent wrapping — everything
	// that refreshViewport would compute. Keyed by compositedKey.
	// When valid, refreshViewport skips all per-entry render work
	// and just concatenates cached strings. Aligned with Claude
	// Code's component-level caching and Codex's committed_line_count.
	composited    string
	compositedKey uint64 // hash of inputs that produced composited
	compositedH   int    // line count of composited (for virtual viewport)

	// streamBuf accumulates content deltas during streaming via
	// WriteString (O(1) amortized). When streaming ends, Content is
	// set to streamBuf.String() once. This replaces the old
	// Content += delta pattern which copied the entire string on
	// every token (O(n²) total).
	streamBuf *strings.Builder

	// Thinking block.
	ThinkingContent  string
	ThinkingDuration time.Duration
	ThinkingDone     bool
	ThinkingExpanded bool

	// Tool calls in this assistant turn.
	ToolCalls []ToolCallEntry

	// blockOrder records the stream-order sequence of content blocks.
	// Each entry is either "text" (for Content segments) or "tool:N"
	// (for ToolCalls[N]). Rendering follows this order to match
	// Claude Code's interleaved display. When empty, falls back to
	// legacy order (thinking → tools → content).
	blockOrder []string

	// textSegmentOffsets tracks byte offsets into Content where each
	// "text" segment begins. Used to split Content into per-segment
	// slices for interleaved rendering. len(textSegmentOffsets) ==
	// number of "text" entries in blockOrder.
	textSegmentOffsets []int
}

type queuedMessage struct {
	Text            string
	Images          []providers.InputImage
	ScheduledTaskID string
}

type pendingTurnResult struct {
	newMsgs              []providers.ChatMessage
	historyRewritten     bool
	incrementalPersisted bool
}

type pendingChatClickState struct {
	active bool
	x      int
	y      int
}

type workerUsageSnapshot struct {
	inputTokens  int
	outputTokens int
}

// Model implements the terminal UI state machine.
type Model struct {
	provider        string
	modelName       string
	configPath      string
	workspaceRoot   string
	memoryPath      string
	stateDir        string
	sessionID       string
	sessionDir      string
	streamRunner    *agent.StreamRunner
	hookDispatcher  *hooks.Dispatcher
	streamCh        chan providers.StreamEvent
	eventBus        *eventbus.Bus
	onSessionID     func(string)
	skills          []skills.Skill
	memoryFiles     []memory.File
	agentControl    *agentcontrol.AgentControl
	processManager  *processruntime.Manager
	processNotifyCh chan processruntime.Event
	workerNotifyCh  chan subagent.Notification

	// Toolkit allows runtime toolset switching for experimental slash modes.
	toolkit                     *tools.Toolkit
	baseSystemPrompt            string
	coordinatorPreamble         string
	experimentalCoordinatorMode bool
	coordinatorMode             bool

	// Cron scheduler: fires scheduled prompts into messageQueue.
	scheduler     *cron.Scheduler
	cronFireCh    chan cron.Task
	schedulerLock *cron.Lock

	// Auto-resume state: when a worker completes while the main agent
	// is busy, we set pendingAutoResume so the streamFinishedMsg
	// handler knows to fire a fresh turn from the existing history.
	// autoResumeChain counts how many auto-turns have fired in a row
	// without user input — used as a runaway safety net.
	pendingAutoResume bool
	autoResumeChain   int

	// pendingWorkerMessages holds structured worker mailbox messages
	// that arrived while a turn was still in flight. They are appended
	// only after the turn's messages have been committed so they cannot
	// land between an assistant tool_call and its tool result.
	pendingWorkerMessages []providers.ChatMessage

	requestTimeout time.Duration

	viewport viewport.Model
	input    textarea.Model

	layout     layout
	inputLines int

	width  int
	height int

	entries     []transcriptEntry
	chatHistory []providers.ChatMessage
	pendingTurn *pendingTurnResult // shared with goroutine for returning turn result

	pendingRequest bool
	streaming      bool
	streamTarget   int
	streamElapsed  time.Duration
	thinkingStart  time.Time // when thinking began for current turn
	spinnerFrame   int

	autoFollow      bool
	showJump        bool
	clock           string
	statusLine      string
	liveWorkStatus  workStatus
	inlineSpinFrame int

	streamCollector *markdown.StreamCollector

	// Slash command completion popup.
	completionVisible bool
	completionItems   []command
	completionIndex   int

	// Cancel in-flight stream on quit.
	cancelStream context.CancelFunc

	// Double ctrl+c to quit.
	ctrlCPressed bool
	quitting     bool

	// Lazy session creation — only write to disk on first message.
	sessionCreated bool

	// Input history — user messages for up/down recall.
	inputHistory []string
	historyIndex int    // -1 = not browsing, 0..len-1 = browsing
	historyDraft string // saves current input when entering history

	// Message queue — Tab queues follow-up messages.
	messageQueue []queuedMessage
	// Steer queue — Enter while busy adds steer messages.
	pendingSteers []queuedMessage

	// Pending image attachments for the next user message.
	pendingImages    []providers.InputImage
	imageBarFocused  bool // true when user is navigating the image bar
	selectedImageIdx int  // index of the selected image pill

	// renderedContent is the full multi-line string most recently
	// passed to viewport.SetContent. We hold our own copy because
	// the bubbletea viewport's View() only returns the visible
	// window, and selection / copy need access to lines that may
	// have scrolled off-screen.
	renderedContent string

	// Cached token estimate for the header, updated only when entries change.
	cachedTokenEstimate int

	// Cached separator line, invalidated on width change.
	cachedSep string

	// Deferred viewport refresh. When the user scrolls away from the
	// active streaming entry, live deltas update transcript state
	// immediately but postpone viewport.SetContent until that entry is
	// visible again (or the user returns to bottom).
	pendingViewportRefresh bool
	pendingViewportEntry   int

	// Text selection in viewport.
	selection selectionState

	// In-viewport search overlay state.
	search searchState

	// Pending click in the chat area. A plain click should focus the
	// input on release; only once motion exceeds a small threshold do
	// we convert it into an actual text-selection drag.
	pendingChatClick pendingChatClickState

	// Text selection in input textarea.
	inputSelection    selectionState
	pendingInputClick pendingChatClickState

	// Auto-scroll state for drag-select past the viewport edge.
	// While the mouse is held outside the chat area, a recurring tick
	// scrolls the viewport in the held direction so the selection can
	// extend into off-screen content (standard editor behavior).
	// `seq` is bumped on every (de)activation so stale in-flight ticks
	// from a previous burst can recognize themselves and exit cleanly
	// instead of compounding into runaway scroll.
	selectionAutoScroll selectionAutoScrollState

	// Token usage accumulator for current session.
	mainInputTokens    int
	mainOutputTokens   int
	workerInputTokens  int
	workerOutputTokens int
	workerUsageByID    map[string]workerUsageSnapshot
	workerSpawnedByID  map[string]bool
	processEventSeen   map[string]bool
	turnInputTokens    int
	turnOutputTokens   int

	// Projected input-token count for the next provider request —
	// system prompt + tool schemas + chat history. Recomputed in
	// refreshViewport so the header can show the live context
	// utilization %. Intentionally slight overestimate (counts tool
	// schemas even on the first turn when no tool_use has happened
	// yet) so the warning/error colour triggers fire a bit early.
	contextTokens int

	// Insight generation state.
	insightRunning     bool
	insightCh          chan insight.ProgressEvent
	cancelInsight      context.CancelFunc
	insightProgressIdx int // index of the live progress entry in entries, -1 if none

	// Resume picker (modal sub-screen).
	resumePicker *resumePicker

	// Ask-user bridge + active modal. When the main agent calls the
	// ask_user tool, the bridge publishes a pending request to
	// askBridge.Requests(); a tea.Cmd reads it and delivers an
	// askRequestMsg which wires up activeAsk. While activeAsk != nil
	// the modal takes over key routing and View rendering, same
	// pattern as resumePicker.
	askBridge *AskUserBridge
	activeAsk *askUserModal
}
