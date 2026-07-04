package appserver

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func resetParticipantForTest(t *testing.T, srv *Server, reqID, participantID, scope string) map[string]any {
	t.Helper()
	raw := fmt.Sprintf(`{"id":%q,"method":"participant/reset","params":{"participant_id":%q,"scope":%q}}`, reqID, participantID, scope)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("participant/reset: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	return responseByID(t, msgs, reqID)
}

// The "restart" reset scope used to be an empty case that reported success
// without doing anything (consistency repair plan §1 #6). It is retired:
// the server now answers with an explicit error instead of lying.
func TestParticipantResetRejectsRestartScope(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")

	resp := resetParticipantForTest(t, srv, "reset-restart", ivy, "restart")
	errMsg, ok := resp["error"]
	if !ok {
		t.Fatalf("expected error for restart scope, got: %+v", resp)
	}
	errStr := fmt.Sprint(errMsg)
	if !strings.Contains(errStr, "restart scope is no longer supported") {
		t.Fatalf("error should say restart scope is no longer supported, got %q", errStr)
	}
}

// With the "restart" default gone, an empty scope no longer silently
// succeeds: it is rejected as unsupported like any other unknown scope.
func TestParticipantResetRejectsEmptyScope(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")

	resp := resetParticipantForTest(t, srv, "reset-empty", ivy, "")
	errMsg, ok := resp["error"]
	if !ok {
		t.Fatalf("expected error for empty scope, got: %+v", resp)
	}
	errStr := fmt.Sprint(errMsg)
	if !strings.Contains(errStr, "unsupported reset scope") {
		t.Fatalf("error should say the scope is unsupported, got %q", errStr)
	}
}
