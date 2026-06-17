package appserver

import (
	"context"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// TestBuildGuardianReviewer_NilServer verifies the helper is nil-safe so
// callers never have to special-case it.
func TestBuildGuardianReviewer_NilServer(t *testing.T) {
	//nolint:staticcheck // explicit nil-receiver test
	var s *Server
	kit, err := tools.New(t.TempDir())
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	if reviewer, ok := s.buildGuardianReviewer(kit); ok || reviewer != nil {
		t.Fatalf("nil server should return (nil, false); got (%+v, %v)", reviewer, ok)
	}
}

// TestBuildGuardianReviewer_NilRT verifies the helper refuses to fabricate
// a guardian when there is no runtime session to source provider config
// from. The caller must fail closed in this case.
func TestBuildGuardianReviewer_NilRT(t *testing.T) {
	s := &Server{}
	kit, err := tools.New(t.TempDir())
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	if reviewer, ok := s.buildGuardianReviewer(kit); ok || reviewer != nil {
		t.Fatalf("nil rt should return (nil, false); got (%+v, %v)", reviewer, ok)
	}
}

// TestBuildGuardianReviewer_BadConfigDir verifies the helper reports a
// construction failure (here: LoadFrom can't find any config) via the
// ok=false return value rather than panicking.
func TestBuildGuardianReviewer_BadConfigDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{
		rt: &runtime.Session{
			RootDir:      "/this/path/definitely/does/not/exist/wuu-test",
			ProviderName: "anything",
		},
	}
	kit, err := tools.New(t.TempDir())
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	if reviewer, ok := s.buildGuardianReviewer(kit); ok || reviewer != nil {
		t.Fatalf("bad config dir should return (nil, false); got (%+v, %v)", reviewer, ok)
	}
}

// TestInstallToolApprovalReviewer_NilRTKeepsIPCPath verifies that when
// the runtime session is missing we keep the legacy user-prompt path
// rather than attempting to wire a guardian or fall back silently.
func TestInstallToolApprovalReviewer_NilRTKeepsIPCPath(t *testing.T) {
	s := &Server{}
	kit, err := tools.New(t.TempDir())
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	s.installToolApprovalReviewer(kit)
	if s.lastFallback != "" {
		t.Fatalf("nil rt should not record auto-review failure; got %q", s.lastFallback)
	}
	if kit.ApprovalStore() == nil {
		t.Fatal("SetToolApprovalReviewer should ensure the store exists")
	}
}

// TestInstallToolApprovalReviewer_AutoReviewFailsClosedOnProviderFailure
// verifies the Codex-style contract: when auto_review is configured but
// the guardian cannot be built, approval requests are blocked instead of
// falling back to a local rule engine that might approve the action.
func TestInstallToolApprovalReviewer_AutoReviewFailsClosedOnProviderFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{
		rt: &runtime.Session{
			RootDir: "/this/path/definitely/does/not/exist/wuu-test",
			Permissions: config.ResolvedPermissions{
				ApprovalsReviewer: config.ApprovalsReviewerAutoReview,
			},
		},
	}
	kit, err := tools.New(t.TempDir())
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	kit.SetToolPolicy(tools.ToolPolicy{
		ToolActions: map[string]tools.ToolPolicyAction{
			"write_file": tools.ToolPolicyRequireApproval,
		},
	})
	s.installToolApprovalReviewer(kit)
	if !strings.Contains(s.lastFallback, "auto_review guardian unavailable") {
		t.Fatalf("expected guardian failure reason, got %q", s.lastFallback)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-write",
		Name:      "write_file",
		Arguments: `{"path":"notes.txt","content":"hello\n","create_only":true}`,
	})
	if err != nil {
		if !strings.Contains(err.Error(), "error_kind=approval_denied") ||
			!strings.Contains(err.Error(), "auto_review guardian unavailable") {
			t.Fatalf("unexpected denial error: %v", err)
		}
	} else {
		t.Fatal("expected guardian-unavailable auto_review to deny low-risk write")
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 || records[0].ApprovalDecision != tools.ToolApprovalDecisionDenied {
		t.Fatalf("approval telemetry = %+v, want one denial", records)
	}
}

// TestLogGuardianFallback_NilSafe verifies the warning recorder is safe to
// call with any combination of nil receiver, nil writer, or empty msg.
func TestLogGuardianFallback_NilSafe(t *testing.T) {
	//nolint:staticcheck // explicit nil-receiver test
	var nilServer *Server
	nilServer.logGuardianFallback("anything") // must not panic

	s := &Server{}
	s.logGuardianFallback("") // empty msg is suppressed, no write
	if s.lastFallback != "" {
		t.Fatalf("empty msg should not record fallback, got %q", s.lastFallback)
	}
	s.logGuardianFallback("hello")
	if s.lastFallback != "hello" {
		t.Fatalf("expected recorded reason, got %q", s.lastFallback)
	}
}
