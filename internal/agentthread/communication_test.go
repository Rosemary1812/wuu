package agentthread

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestInterAgentCommunicationChangedFileOverlapRoundTrip ensures the new
// ChangedFileOverlap sibling serializes alongside the rest of the envelope
// without leaving a text-after-JSON tail — the wire shape the sub-agent
// completion wakeup relies on after the structural-overlap migration.
func TestInterAgentCommunicationChangedFileOverlapRoundTrip(t *testing.T) {
	warnings := []string{
		"changed_file_overlap: foo.go touched by /root/a, /root/b",
		"changed_file_overlap: bar.go touched by /root/a",
	}
	comm := NewInterAgentCommunication(
		AgentPath("/root/research"),
		AgentPath("/root"),
		"<subagent_notification>\n{\"agent_path\":\"/root/research\"}\n</subagent_notification>",
		true,
	)
	comm.ChangedFileOverlap = warnings

	raw := comm.String()
	if strings.Contains(raw, "<changed_file_overlap>") {
		t.Fatalf("envelope should not embed the legacy text-tail tag: %s", raw)
	}
	if !json.Valid([]byte(raw)) {
		t.Fatalf("envelope is not valid JSON: %s", raw)
	}

	var decoded InterAgentCommunication
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, raw)
	}
	if decoded.Author != comm.Author || decoded.Recipient != comm.Recipient || decoded.TriggerTurn != comm.TriggerTurn || decoded.Content != comm.Content {
		t.Fatalf("envelope did not round-trip core fields: got %+v want %+v", decoded, comm)
	}
	if got := decoded.ChangedFileOverlap; len(got) != len(warnings) {
		t.Fatalf("ChangedFileOverlap length = %d, want %d", len(got), len(warnings))
	} else {
		for i, want := range warnings {
			if got[i] != want {
				t.Fatalf("ChangedFileOverlap[%d] = %q, want %q", i, got[i], want)
			}
		}
	}
}

// TestInterAgentCommunicationOmitsEmptyOverlap ensures the omitempty tag
// keeps unchanged envelopes wire-compatible with pre-sibling consumers.
func TestInterAgentCommunicationOmitsEmptyOverlap(t *testing.T) {
	comm := NewInterAgentCommunication(
		AgentPath("/root/research"),
		AgentPath("/root"),
		"<subagent_notification>\n{\"agent_path\":\"/root/research\"}\n</subagent_notification>",
		false,
	)
	raw := comm.String()
	if strings.Contains(raw, "changed_file_overlap") {
		t.Fatalf("envelope with no warnings should omit the field entirely: %s", raw)
	}
}
