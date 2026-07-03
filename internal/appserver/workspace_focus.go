package appserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/workspaces"
)

// focusWorkspaceHome is the reserved focus value that pins a chat-style
// thread to the agent's home directory only
// (2026-07-03-workspace-focus.md §1). The empty string means "all
// registered workspaces" (the default); any other value must name a
// registered workspace.
const focusWorkspaceHome = "~"

const (
	focusMetaKindAll       = "all"
	focusMetaKindHome      = "home"
	focusMetaKindWorkspace = "workspace"
)

// focusMeta is the structured metadata attached to a workspace-focus
// declaration item (2026-07-03-workspace-focus.md §3.1). It rides the
// focus_meta history column so the chat view can render a focus divider
// without re-parsing the model-visible declaration text.
type focusMeta struct {
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
	Root string `json:"root,omitempty"`
}

// declaration renders the model-visible content of a focus declaration
// item. The format is load-bearing for cache correctness: it is persisted
// into durable history and must reproduce byte-identically on every
// prompt rebuild.
func (m focusMeta) declaration() string {
	switch m.Kind {
	case focusMetaKindHome:
		return "[focus: home directory only]"
	case focusMetaKindWorkspace:
		return fmt.Sprintf("[focus: %s — %s]", m.Name, m.Root)
	default:
		return "[focus: all registered workspaces]"
	}
}

// resolveFocusWorkspace validates a requested focus value against the
// registered workspace roster and resolves its structured metadata.
// homeRoot is the thread's agent home directory (used as the root of a
// "home" declaration). The function is deliberately thread-kind agnostic
// — it sees only strings and the roster — so the group-thread worker can
// reuse it unchanged.
func resolveFocusWorkspace(requested, homeRoot string, roster []workspaces.Workspace) (string, focusMeta, error) {
	requested = strings.TrimSpace(requested)
	switch requested {
	case "":
		return "", focusMeta{Kind: focusMetaKindAll}, nil
	case focusWorkspaceHome:
		return focusWorkspaceHome, focusMeta{Kind: focusMetaKindHome, Root: homeRoot}, nil
	}
	for _, ws := range roster {
		if ws.Name == requested {
			return requested, focusMeta{Kind: focusMetaKindWorkspace, Name: ws.Name, Root: ws.Root}, nil
		}
	}
	return "", focusMeta{}, fmt.Errorf("unknown focus workspace %q: not a registered workspace name (use \"\" for all workspaces or %q for the home directory)", requested, focusWorkspaceHome)
}

// focusWorkspaceRoot maps a stored focus value to the tool execution root
// for a chat-style thread (2026-07-03-workspace-focus.md §5): a workspace
// focus roots tools at that workspace, while "" (all) and "~" (home) keep
// the agent home. A focus naming a workspace that has since been
// unregistered falls back to the agent home rather than failing the turn.
func focusWorkspaceRoot(focus, homeRoot string, roster []workspaces.Workspace) string {
	focus = strings.TrimSpace(focus)
	if focus == "" || focus == focusWorkspaceHome {
		return homeRoot
	}
	for _, ws := range roster {
		if ws.Name == focus {
			return ws.Root
		}
	}
	return homeRoot
}

// focusDeclarationMessage builds the persistent user-role declaration item
// injected when a thread's focus changes. Content doubles as the display
// text; focus_meta carries the structured form for the chat view divider.
func focusDeclarationMessage(meta focusMeta) (providers.ChatMessage, error) {
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return providers.ChatMessage{}, fmt.Errorf("marshal focus meta: %w", err)
	}
	return providers.ChatMessage{
		Role:      "user",
		Content:   meta.declaration(),
		FocusMeta: metaJSON,
	}, nil
}

// applyTurnWorkspaceFocus handles TurnStartParams.FocusWorkspace for a
// chat-style thread (2026-07-03-workspace-focus.md §3): nil leaves the
// focus untouched; a value equal to the stored focus is a no-op
// (idempotent, so callers may send it on every turn); a different value
// is validated against the roster, persisted, and declared into durable
// history as a completed synthetic turn ahead of the caller's user
// message. Invalid workspace names fail the turn before any state
// changes.
func (s *Server) applyTurnWorkspaceFocus(th *threadState, requested *string) error {
	if s == nil || s.rt == nil || th == nil || requested == nil {
		return nil
	}

	th.mu.Lock()
	current := strings.TrimSpace(th.FocusWorkspace)
	homeRoot := th.CWD
	th.mu.Unlock()

	if strings.TrimSpace(*requested) == current {
		return nil
	}
	roster, err := workspaces.List(s.rt.WuuHome)
	if err != nil {
		return fmt.Errorf("load workspace roster: %w", err)
	}
	focus, meta, err := resolveFocusWorkspace(*requested, homeRoot, roster)
	if err != nil {
		return err
	}
	if focus == current {
		return nil
	}

	declMsg, err := focusDeclarationMessage(meta)
	if err != nil {
		return err
	}

	if _, err := session.SetFocusWorkspace(s.rt.SessionDir, th.ID, focus); err != nil {
		return err
	}

	now := time.Now().UTC()
	th.mu.Lock()
	if th.PersistHistory {
		if err := appendChatMessage(s.rt.SessionDir, th.ID, declMsg); err != nil {
			th.mu.Unlock()
			return err
		}
	}
	th.FocusWorkspace = focus
	history := cloneHistory(th.History)
	history = append(history, declMsg)
	th.History = history
	turn := th.appendUserMessageTurnLocked(session.NewID(), declMsg, now)
	th.mu.Unlock()

	_ = s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
		ThreadID: th.ID,
		Turn:     turn,
	})
	return nil
}
