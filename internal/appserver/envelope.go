package appserver

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MessageEnvelope is the compact unit that enters a resident agent's
// context. One envelope carries exactly one new message -- never room
// history. Agents pull surrounding context with fetch_thread_messages.
type MessageEnvelope struct {
	ID                  string    `json:"id"`
	SourceThreadID      string    `json:"source_thread_id"`
	SourceTitle         string    `json:"source_title"`
	SenderKind          string    `json:"sender_kind"`
	SenderName          string    `json:"sender_name"`
	SenderParticipantID string    `json:"sender_participant_id,omitempty"`
	Addressed           bool      `json:"addressed"`
	Hop                 int       `json:"hop"`
	// SourceSeq is the seq of the source message in SourceThreadID — its
	// stable per-thread address. Carried so a resident's read receipt (turn
	// completed/failed on this message) and its optional reaction can point
	// back at the exact message. 0 when the source seq was unknown at routing
	// time (e.g. a non-persisted or non-group-chat message).
	SourceSeq int       `json:"source_seq,omitempty"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	// Workspace snapshots the source thread's stored workspace focus at
	// routing time (2026-07-03-workspace-focus.md "carry source-thread
	// workspace focus on envelopes"): "" for all registered workspaces, "~"
	// for the source thread's home directory, otherwise a registered
	// workspace name. The envelope is self-contained and carries this on
	// every message rather than only on change, because a resident's inbox
	// interleaves messages from many source threads with independent focus
	// state — there is no single "current focus" to diff against.
	Workspace string `json:"workspace,omitempty"`
}

// Prompt renders the envelope into the user-role message injected into
// the resident thread. Format is load-bearing: the resident system prompt
// teaches the agent to read these attributes. The workspace attribute is
// omitted entirely when the source thread has no focus declared ("all
// registered workspaces"); "~" (home) renders as workspace="home".
func (e MessageEnvelope) Prompt() string {
	attrs := fmt.Sprintf(
		"thread=%q thread_id=%q from=%q sender=%q addressed=%q hop=%q",
		e.SourceTitle, e.SourceThreadID, e.SenderKind, e.SenderName,
		strconv.FormatBool(e.Addressed), strconv.Itoa(e.Hop),
	)
	if ws := envelopeWorkspaceAttr(e.Workspace); ws != "" {
		attrs += fmt.Sprintf(" workspace=%q", ws)
	}
	return fmt.Sprintf(
		"<incoming_message %s>\n%s\n</incoming_message>",
		attrs, strings.TrimSpace(e.Text),
	)
}

// envelopeWorkspaceAttr renders MessageEnvelope.Workspace into the
// <incoming_message workspace="..."> attribute value. "" (all workspaces)
// has no attribute at all; focusWorkspaceHome ("~") reads more plainly to
// the agent as "home"; any other value is a registered workspace name,
// rendered verbatim.
func envelopeWorkspaceAttr(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	switch workspace {
	case "":
		return ""
	case focusWorkspaceHome:
		return "home"
	default:
		return workspace
	}
}

func coalesceEnvelopes(envs []MessageEnvelope) string {
	prompts := make([]string, 0, len(envs))
	for _, env := range envs {
		prompt := strings.TrimSpace(env.Prompt())
		if prompt != "" {
			prompts = append(prompts, prompt)
		}
	}
	if len(prompts) == 0 {
		return ""
	}
	joined := strings.Join(prompts, "\n\n")
	if len(prompts) == 1 {
		return joined
	}
	return fmt.Sprintf("You received %d messages while busy:\n\n%s", len(prompts), joined)
}
