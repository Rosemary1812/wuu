package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/blueberrycongee/wuu/internal/cron"
	"github.com/blueberrycongee/wuu/internal/eventbus"
	"github.com/blueberrycongee/wuu/internal/hooks"
	"github.com/blueberrycongee/wuu/internal/insight"
	processruntime "github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

// model_init.go contains Model construction and initialization helpers.
// NewModel builds the initial UI model.
func NewModel(cfg Config) Model {
	vp := viewport.New(80, minOutputHeight)
	vp.SetContent("")
	vp.MouseWheelDelta = 3

	in := defaultInputTextarea
	workspaceRoot := strings.TrimSpace(cfg.WorkspaceRoot)
	if workspaceRoot == "" {
		workspaceRoot = filepath.Dir(cfg.ConfigPath)
	}
	stateDir := strings.TrimSpace(cfg.StateDir)
	if stateDir == "" {
		if home, err := statepath.Home(""); err == nil {
			if dir, err := statepath.WorkspaceDir(home, workspaceRoot); err == nil {
				stateDir = dir
			}
		}
	}

	m := Model{
		provider:                    cfg.Provider,
		modelName:                   cfg.Model,
		configPath:                  cfg.ConfigPath,
		workspaceRoot:               workspaceRoot,
		memoryPath:                  cfg.MemoryPath,
		stateDir:                    stateDir,
		sessionDir:                  cfg.SessionDir,
		streamRunner:                cfg.StreamRunner,
		hookDispatcher:              cfg.HookDispatcher,
		onSessionID:                 cfg.OnSessionID,
		skills:                      cfg.Skills,
		memoryFiles:                 cfg.Memory,
		agentControl:                cfg.AgentControl,
		processManager:              cfg.ProcessManager,
		askBridge:                   cfg.AskUserBridge,
		requestTimeout:              cfg.RequestTimeout,
		toolkit:                     cfg.Toolkit,
		baseSystemPrompt:            cfg.BaseSystemPrompt,
		coordinatorPreamble:         cfg.CoordinatorPreamble,
		experimentalCoordinatorMode: cfg.ExperimentalCoordinatorMode,
		viewport:                    vp,
		input:                       in,
		autoFollow:                  true,
		clock:                       time.Now().Format("15:04:05"),
		statusLine:                  "ready",
		pendingViewportEntry:        -1,
		streamTarget:                -1,
		workerUsageByID:             make(map[string]workerUsageSnapshot),
		workerSpawnedByID:           make(map[string]bool),
		processEventSeen:            make(map[string]bool),
		historyIndex:                -1,
		insightProgressIdx:          -1,
	}

	// Initialise the event bus and wire it into the stream runner so
	// core agent events are available to multiple consumers (TUI,
	// headless mode, future web frontend, etc.).
	m.eventBus = eventbus.New()
	if m.streamRunner != nil {
		m.streamRunner.Bus = m.eventBus
	}

	// Session isolation: create or resume session.
	if m.sessionDir != "" {
		if cfg.ResumeID != "" {
			// Resume existing session.
			path, err := session.Load(m.sessionDir, cfg.ResumeID)
			if err == nil {
				m.sessionID = cfg.ResumeID
				m.memoryPath = path
				m.sessionCreated = true // already on disk
			} else {
				m.statusLine = fmt.Sprintf("resume failed: %v", err)
			}
		}
		if m.sessionID == "" {
			// Generate session ID but don't write to disk yet.
			// Files are created lazily on first message (see ensureSessionFile).
			m.sessionID = session.NewID()
			m.memoryPath = session.FilePath(m.sessionDir, m.sessionID)
		}
		if m.onSessionID != nil && m.sessionID != "" {
			m.onSessionID(m.sessionID)
		}
	}

	// Subscribe to coordinator worker notifications, if a coordinator
	// is wired up. The channel is drained by waitWorkerNotify (a tea.Cmd
	// returned from Init / Update).
	if m.agentControl != nil {
		m.workerNotifyCh = make(chan subagent.Notification, 64)
		m.agentControl.Subscribe(m.workerNotifyCh)
	}
	if m.processManager != nil {
		m.processNotifyCh = make(chan processruntime.Event, 64)
		m.processManager.Subscribe(m.processNotifyCh)
	}

	// Seed chatHistory with the system prompt so every API call includes it.
	if m.streamRunner != nil && strings.TrimSpace(m.streamRunner.SystemPrompt) != "" {
		m.chatHistory = append(m.chatHistory, providers.ChatMessage{
			Role:    "system",
			Content: m.streamRunner.SystemPrompt,
		})
	}

	return m.loadMemory()
}

func (m *Model) resetChatHistory() {
	m.chatHistory = nil
	if m.streamRunner != nil && strings.TrimSpace(m.streamRunner.SystemPrompt) != "" {
		m.chatHistory = append(m.chatHistory, providers.ChatMessage{
			Role:    "system",
			Content: m.streamRunner.SystemPrompt,
		})
	}
}

// setCoordinatorMode switches between normal mode (main agent has full tools)
// and coordinator mode (main agent is read-only, delegates to workers).
// It updates the toolkit, system prompt, and chatHistory in place.
func (m *Model) setCoordinatorMode(enabled bool) string {
	if m.toolkit == nil {
		return "coordinator mode: toolkit not available"
	}
	if m.streaming || m.pendingRequest {
		return "coordinator mode: cannot switch while a response is in progress"
	}
	if enabled && m.agentControl == nil {
		return "coordinator mode: coordinator runtime not available (not a git repository?)"
	}
	if m.coordinatorMode == enabled {
		if enabled {
			return "already in coordinator mode"
		}
		return "already in normal mode"
	}

	m.coordinatorMode = enabled
	if enabled {
		m.toolkit.DisableTools("write_file", "edit_file", "run_shell")
	} else {
		m.toolkit.EnableTools("write_file", "edit_file", "run_shell")
	}

	// Rebuild system prompt.
	newPrompt := m.baseSystemPrompt
	if enabled && m.coordinatorPreamble != "" {
		newPrompt = m.coordinatorPreamble + "\n\n" + newPrompt
	}
	if m.streamRunner != nil {
		m.streamRunner.UpdateSystemPrompt(newPrompt)
	}

	// Update the existing system message in chatHistory if present.
	if len(m.chatHistory) > 0 && m.chatHistory[0].Role == "system" {
		m.chatHistory[0].Content = newPrompt
	}

	if enabled {
		return "entered coordinator mode — write tools disabled, orchestration active"
	}
	return "returned to normal mode — write tools enabled"
}

func finishInputTextareaSetup(in *textarea.Model) {
	in.Placeholder = "Ask anything..."
	in.Focus()
	in.SetWidth(80)
	in.SetHeight(3)
	in.ShowLineNumbers = false
	in.Prompt = "> "
	in.CharLimit = 0
	applyInputTextareaTheme(in)
}

func newInputTextarea() textarea.Model {
	in := textarea.New()
	finishInputTextareaSetup(&in)
	return in
}

func refreshTextareasForTheme() {
	defaultInputTextarea = newInputTextarea()
	defaultOnboardingTextarea = newOnboardingTextarea()
}

func applyInputTextareaTheme(in *textarea.Model) {
	focused := in.FocusedStyle
	focused.Base = lipgloss.NewStyle()
	focused.CursorLine = lipgloss.NewStyle()
	focused.CursorLineNumber = lipgloss.NewStyle().Foreground(currentTheme.Subtle)
	focused.EndOfBuffer = lipgloss.NewStyle().Foreground(currentTheme.Inactive)
	focused.LineNumber = lipgloss.NewStyle().Foreground(currentTheme.Subtle)
	focused.Placeholder = lipgloss.NewStyle().Foreground(currentTheme.Inactive)
	focused.Prompt = lipgloss.NewStyle().Foreground(currentTheme.Brand)
	focused.Text = lipgloss.NewStyle().Foreground(currentTheme.Text)

	blurred := in.BlurredStyle
	blurred.Base = lipgloss.NewStyle()
	blurred.CursorLine = lipgloss.NewStyle()
	blurred.CursorLineNumber = lipgloss.NewStyle().Foreground(currentTheme.Subtle)
	blurred.EndOfBuffer = lipgloss.NewStyle().Foreground(currentTheme.Inactive)
	blurred.LineNumber = lipgloss.NewStyle().Foreground(currentTheme.Subtle)
	blurred.Placeholder = lipgloss.NewStyle().Foreground(currentTheme.Inactive)
	blurred.Prompt = lipgloss.NewStyle().Foreground(currentTheme.Subtle)
	blurred.Text = lipgloss.NewStyle().Foreground(currentTheme.Text)

	in.FocusedStyle = focused
	in.BlurredStyle = blurred
}

func (m Model) loadMemory() Model {
	if strings.TrimSpace(m.memoryPath) == "" {
		return m
	}

	// Load structured history first. This may repair and rewrite old
	// sessions before the transcript view is reconstructed from disk.
	chatMsgs, chatErr := loadChatHistory(m.memoryPath)
	if chatErr == nil && len(chatMsgs) > 0 {
		// If we already have a system prompt in chatHistory, keep it and append loaded messages.
		if len(m.chatHistory) > 0 && m.chatHistory[0].Role == "system" {
			m.chatHistory = append(m.chatHistory[:1], chatMsgs...)
		} else {
			m.chatHistory = chatMsgs
		}
	}

	entries, err := loadMemoryEntries(m.memoryPath)
	if err != nil {
		m.statusLine = fmt.Sprintf("memory load failed: %v", err)
		return m
	}
	if len(entries) > 0 {
		m.entries = append(m.entries, entries...)

		// Populate input history from loaded user messages.
		for _, e := range entries {
			if e.Role == "USER" {
				content := strings.TrimSpace(stripUserImagePlaceholderLines(e.Content))
				if content != "" && content != "(empty)" {
					m.inputHistory = append(m.inputHistory, content)
				}
			}
		}

		m.statusLine = fmt.Sprintf("resumed %d entries", len(entries))
	}
	m.loadPersistedTokenUsage()
	m.cacheRenderedEntries()
	m.refreshViewport(true)

	// Start cron scheduler for durable tasks.
	if m.stateDir == "" {
		return m
	}
	schedPath := statepath.ScheduledTasksPath(m.stateDir)
	lockPath := statepath.ScheduledTasksLockPath(m.stateDir)
	schedStore := cron.NewTaskStore(schedPath)
	sessionStore := cron.NewSessionTaskStore(m.stateDir)
	m.cronFireCh = make(chan cron.Task, 8)
	m.schedulerLock = cron.NewLock(lockPath, m.sessionID)
	m.scheduler = cron.NewScheduler(cron.SchedulerConfig{
		Store:        schedStore,
		SessionStore: sessionStore,
		OnFire: func(task cron.Task) {
			select {
			case m.cronFireCh <- task:
			default:
			}
		},
		IsOwner: func() bool {
			ok, _ := m.schedulerLock.TryAcquire()
			return ok
		},
	})
	m.scheduler.Start()

	return m
}

// Init starts the clock ticker.
// dispatchSessionEnd fires the SessionEnd hook with a short timeout.
func (m Model) dispatchSessionEnd() {
	if m.hookDispatcher == nil || !m.hookDispatcher.HasHooks(hooks.SessionEnd) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	m.hookDispatcher.Dispatch(ctx, hooks.SessionEnd, &hooks.Input{
		SessionID: m.sessionID,
		CWD:       m.workspaceRoot,
	})
}

func (m Model) Init() tea.Cmd {
	// Dispatch SessionStart hook (fire-and-forget).
	if m.hookDispatcher != nil && m.hookDispatcher.HasHooks(hooks.SessionStart) {
		go m.hookDispatcher.Dispatch(context.Background(), hooks.SessionStart, &hooks.Input{
			SessionID: m.sessionID,
			CWD:       m.workspaceRoot,
		})
	}
	cmds := []tea.Cmd{tickCmd(), statusAnimationCmd()}
	if m.workerNotifyCh != nil {
		cmds = append(cmds, waitWorkerNotify(m.workerNotifyCh))
	}
	if m.askBridge != nil {
		cmds = append(cmds, waitAskRequest(m.askBridge.Requests()))
	}
	if m.processManager != nil {
		cmds = append(cmds, processPollCmd())
		cmds = append(cmds, waitProcessNotify(m.processNotifyCh))
	}
	if m.cronFireCh != nil {
		cmds = append(cmds, waitCronFire(m.cronFireCh))
	}
	return tea.Batch(cmds...)
}

// waitWorkerNotify reads one notification from the worker channel and
// turns it into a workerNotifyMsg.
func waitWorkerNotify(ch <-chan subagent.Notification) tea.Cmd {
	return func() tea.Msg {
		n, ok := <-ch
		if !ok {
			return nil
		}
		return workerNotifyMsg{notification: n}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg{now: t}
	})
}

func statusAnimationCmd() tea.Cmd {
	return tea.Tick(statusAnimationInterval, func(_ time.Time) tea.Msg {
		return inlineSpinMsg{}
	})
}

func inlineSpinTickCmd() tea.Cmd {
	return tea.Tick(statusAnimationInterval, func(_ time.Time) tea.Msg {
		return inlineSpinMsg{}
	})
}

func waitProcessNotify(ch <-chan processruntime.Event) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return nil
		}
		return processNotifyMsg{event: event}
	}
}

func processPollCmd() tea.Cmd {
	return tea.Tick(time.Second, func(_ time.Time) tea.Msg {
		return processPollMsg{}
	})
}

// selectionAutoScrollInterval is the cadence of the auto-scroll tick
// fired while a drag-select is held outside the chat viewport. Fast
// enough to feel responsive but slow enough that the per-tick line
// jump (capped by selectionAutoScrollMaxSpeed) gives the user time
// to release before overshooting. ~25 lines/second at 1 line/tick.
const selectionAutoScrollInterval = 40 * time.Millisecond

// selectionAutoScrollMaxSpeed caps how many content lines a single
// tick may advance, so even an extreme drag (mouse parked far below
// the terminal) doesn't blast through hundreds of lines instantly.
const selectionAutoScrollMaxSpeed = 5

func selectionAutoScrollCmd(seq int) tea.Cmd {
	return tea.Tick(selectionAutoScrollInterval, func(_ time.Time) tea.Msg {
		return selectionAutoScrollMsg{seq: seq}
	})
}

// applyResume loads the chosen session into the current Model, replacing
// current entries and chat history. Used by both the picker and direct
// /resume <id> invocation.
func (m Model) applyResume(id string) (tea.Model, tea.Cmd) {
	if m.sessionDir == "" {
		m.statusLine = "resume: no session directory configured"
		return m, nil
	}
	path, err := session.Load(m.sessionDir, id)
	if err != nil {
		m.statusLine = fmt.Sprintf("resume: %v", err)
		m.refreshViewport(false)
		return m, nil
	}
	// Repair the persisted history before rebuilding transcript UI.
	chatMsgs, chatErr := loadChatHistory(path)
	entries, err := loadMemoryEntries(path)
	if err != nil {
		m.statusLine = fmt.Sprintf("resume: failed to load: %v", err)
		m.refreshViewport(false)
		return m, nil
	}
	m.sessionID = id
	m.memoryPath = path
	m.entries = entries
	m.workerUsageByID = make(map[string]workerUsageSnapshot)
	m.workerSpawnedByID = make(map[string]bool)
	m.mainInputTokens = 0
	m.mainOutputTokens = 0
	m.workerInputTokens = 0
	m.workerOutputTokens = 0
	m.loadPersistedTokenUsage()
	m.cacheRenderedEntries()

	// Reload chat history for API calls.
	if chatErr == nil && len(chatMsgs) > 0 {
		if len(m.chatHistory) > 0 && m.chatHistory[0].Role == "system" {
			m.chatHistory = append(m.chatHistory[:1], chatMsgs...)
		} else {
			m.chatHistory = chatMsgs
		}
	}

	if m.onSessionID != nil {
		m.onSessionID(id)
	}
	m.statusLine = fmt.Sprintf("resumed %s (%d entries)", id, len(entries))
	m.refreshViewport(true)
	return m, nil
}

func waitInsightEvent(ch <-chan insight.ProgressEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return insightFinishedMsg{}
		}
		return insightProgressMsg{event: event}
	}
}

func waitCronFire(ch <-chan cron.Task) tea.Cmd {
	return func() tea.Msg {
		return cronFireMsg{task: <-ch}
	}
}

// progressBar renders a text progress bar like [████░░░░░░] 45%
func progressBar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	filled := int(pct * float64(width))
	if filled > width {
		filled = width
	}
	empty := width - filled
	return fmt.Sprintf("[%s%s] %2d%%",
		strings.Repeat("█", filled),
		strings.Repeat("░", empty),
		int(pct*100))
}
