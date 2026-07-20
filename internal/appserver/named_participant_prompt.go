package appserver

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/blueberrycongee/wuu/internal/memdir"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/workspaces"
)

func namedParticipantPrompt(p participant.Participant, memory, prompt string, registered []workspaces.Workspace) string {
	name := firstNonEmpty(strings.TrimSpace(p.Name), "Named agent")
	var b strings.Builder
	fmt.Fprintf(&b, "You are %s, a persistent named agent executing one assigned request.\n", name)
	if role := strings.TrimSpace(p.Role); role != "" {
		fmt.Fprintf(&b, "Role: %s.\n", role)
	}
	if tagline := strings.TrimSpace(p.Tagline); tagline != "" {
		fmt.Fprintf(&b, "Profile: %s.\n", tagline)
	}
	b.WriteString("Use the standard agent tools to complete the request. Coordinate only through the task and artifact references you receive; do not attempt peer messaging.\n")

	if workspace := strings.TrimSpace(p.Workspace); workspace != "" {
		fmt.Fprintf(&b, "\nYour durable memory notebook is `%s`. Save only knowledge worth carrying into future runs.\n", filepath.Join(workspace, "memory"))
	}
	if memory = strings.TrimSpace(memory); memory != "" {
		b.WriteString("\n## Memory\n")
		b.WriteString(memory)
		b.WriteByte('\n')
	}
	if len(registered) > 0 {
		b.WriteString("\n## Registered workspaces\n")
		for _, workspace := range registered {
			name := firstNonEmpty(strings.TrimSpace(workspace.Name), strings.TrimSpace(workspace.Root))
			fmt.Fprintf(&b, "- %s — %s\n", name, strings.TrimSpace(workspace.Root))
		}
	}
	b.WriteString("\n## Request\n")
	b.WriteString(strings.TrimSpace(prompt))
	return b.String()
}

func (s *Server) registeredWorkspaces() []workspaces.Workspace {
	if s == nil || s.rt == nil {
		return nil
	}
	list, err := workspaces.List(s.rt.WuuHome)
	if err != nil {
		providers.DebugLogf("read registered workspaces: %v", err)
		return nil
	}
	return list
}

func (s *Server) resolvedParticipantWorkspace(p participant.Participant) (string, error) {
	workspace := strings.TrimSpace(p.Workspace)
	if workspace == "" && s != nil {
		return s.participantWorkspace(p.ID)
	}
	return workspace, nil
}

func (s *Server) readParticipantMemory(p participant.Participant) (string, error) {
	workspace, err := s.resolvedParticipantWorkspace(p)
	if err != nil {
		return "", err
	}
	if workspace == "" {
		return "", nil
	}
	snapshot, err := memdir.ReadIndex(filepath.Join(workspace, "memory"))
	if err != nil {
		return "", fmt.Errorf("read participant memory: %w", err)
	}
	if strings.TrimSpace(snapshot.Content) != "" {
		return snapshot.Content, nil
	}
	legacy, err := memdir.ReadIndex(workspace)
	if err != nil {
		return "", fmt.Errorf("read participant memory: %w", err)
	}
	return legacy.Content, nil
}
