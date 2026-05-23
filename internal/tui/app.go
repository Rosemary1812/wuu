package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/hooks"
	"github.com/blueberrycongee/wuu/internal/memory"
	processruntime "github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/skills"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// Config defines runtime dependencies for the interactive UI.
type Config struct {
	Provider                    string
	Model                       string
	WorkspaceRoot               string
	ConfigPath                  string
	MemoryPath                  string
	StateDir                    string // user-level workspace state directory
	SessionDir                  string // user-level sessions directory for conversation logs
	ResumeID                    string // session ID to resume (empty = new session)
	MaxContextTokens            int
	RequestTimeout              time.Duration
	StreamRunner                *agent.StreamRunner
	HookDispatcher              *hooks.Dispatcher          // optional, dispatches lifecycle hooks
	OnSessionID                 func(string)               // optional, called when the session ID changes
	Skills                      []skills.Skill             // discovered skills, for /<skill-name> shorthand
	Memory                      []memory.File              // discovered CLAUDE.md / AGENTS.md files
	AgentControl                *agentcontrol.AgentControl // optional, enables worker status panel + result injection
	AskUserBridge               *AskUserBridge             // optional, enables the ask_user modal dialog
	ProcessManager              *processruntime.Manager    // optional, enables process panel + commands
	Toolkit                     *tools.Toolkit             // optional, underlying toolkit for runtime tool switching
	BaseSystemPrompt            string                     // system prompt without coordinator preamble
	CoordinatorPreamble         string                     // coordinator preamble text
	ExperimentalCoordinatorMode bool                       // opt-in slash mode for coordinator experiments
	CleanupSummary              processruntime.CleanupResult
}

// Run starts the interactive terminal UI.
func Run(cfg Config) error {
	if cfg.StreamRunner == nil {
		return errors.New("stream runner is required")
	}
	if strings.TrimSpace(cfg.Provider) == "" {
		return errors.New("provider is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return errors.New("model is required")
	}
	if strings.TrimSpace(cfg.ConfigPath) == "" {
		return errors.New("config path is required")
	}

	m := NewModel(cfg)
	program := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion())
	finalModel, err := program.Run()
	if err != nil {
		return fmt.Errorf("run tui: %w", err)
	}

	// Print resume hint after exiting alt screen — only if conversation happened.
	if fm, ok := finalModel.(Model); ok {
		if len(cfg.CleanupSummary.Cleaned) > 0 {
			fmt.Println()
			fmt.Printf("Cleaned up %d session process(es):\n", len(cfg.CleanupSummary.Cleaned))
			for _, proc := range cfg.CleanupSummary.Cleaned {
				fmt.Printf("  - %s (%s)\n", proc.Command, proc.ID)
			}
		}
		if fm.sessionID != "" && fm.sessionCreated && len(fm.entries) > 0 {
			fmt.Println()
			fmt.Printf("To resume this session:\n")
			fmt.Printf("  wuu --resume %s\n", fm.sessionID)
			fmt.Println()
		}
	}

	return nil
}
