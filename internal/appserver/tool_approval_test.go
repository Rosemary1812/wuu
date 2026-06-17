package appserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/modelroles"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/reviewsession"
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

func TestBuildGuardianReviewerUsesReviewRoleSelection(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TEST_WUU_KEY", "abc")
	if err := os.WriteFile(filepath.Join(root, ".wuu.json"), []byte(`{
  "default_provider": "custom",
  "providers": {
    "custom": {
      "type": "openai-compatible",
      "base_url": "https://example.test/v1",
      "api_key_env": "TEST_WUU_KEY",
      "model": "main-model",
      "models": {
        "review-alias": {"id": "review-api-model"}
      }
    }
  },
  "agent": {
    "model_roles": {
      "review": {"model": "review-alias", "effort": "low"}
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, _, err := config.LoadFrom(root, home)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	roles, err := modelroles.Resolve(cfg, modelroles.ResolveOptions{})
	if err != nil {
		t.Fatalf("modelroles.Resolve: %v", err)
	}
	kit, err := tools.New(root)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	s := &Server{rt: &runtime.Session{
		RootDir:      root,
		ProviderName: "custom",
		Model:        "main-model",
		ModelRoles:   roles,
	}}

	reviewer, ok := s.buildGuardianReviewer(kit)
	if !ok || reviewer == nil || reviewer.Session == nil {
		t.Fatalf("expected guardian reviewer with review session, got reviewer=%+v ok=%v", reviewer, ok)
	}
	if reviewer.Session.Model() != "review-api-model" || reviewer.Session.Role() != "guardian" {
		t.Fatalf("guardian should use review role API model, got model=%q role=%q", reviewer.Session.Model(), reviewer.Session.Role())
	}
	boundary := reviewer.Session.Boundary()
	if boundary.PermissionProfile != reviewsession.PermissionProfileReadOnly ||
		boundary.ApprovalPolicy != reviewsession.ApprovalPolicyNever ||
		boundary.Tools || boundary.MCP || boundary.Hooks || boundary.Plugins || boundary.Skills || boundary.MemoryWrites || boundary.DurableWrites {
		t.Fatalf("guardian review session should be restricted: %+v", boundary)
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
