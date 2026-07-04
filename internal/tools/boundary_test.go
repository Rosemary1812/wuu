package tools

import (
	"errors"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

type testGuard struct {
	err error
}

func (g testGuard) Check(ToolInfo, providers.ToolCall) error {
	return g.err
}

func TestStandardBoundaryAllowsMutation(t *testing.T) {
	b := StandardBoundary()
	err := b.Check(ToolInfo{Name: "write_file", Kind: ToolKindFile}, providers.ToolCall{Name: "write_file"})
	if err != nil {
		t.Fatalf("standard boundary should allow mutations: %v", err)
	}
	if !b.Enforce || !b.AllowMutations {
		t.Fatalf("standard boundary = %+v, want enforced mutations allowed", b)
	}
}

func TestReadOnlyBoundaryRejectsMutationAndAllowsRead(t *testing.T) {
	b := ReadOnlyBoundary()
	err := b.Check(ToolInfo{Name: "write_file", Kind: ToolKindFile}, providers.ToolCall{Name: "write_file"})
	if err == nil || !strings.Contains(err.Error(), "error_kind=boundary_denied") {
		t.Fatalf("read-only boundary should deny file mutation, got %v", err)
	}

	err = b.Check(ToolInfo{Name: "read_file", Kind: ToolKindFile, ReadOnly: true}, providers.ToolCall{Name: "read_file"})
	if err != nil {
		t.Fatalf("read-only boundary should allow reads: %v", err)
	}
}

func TestUnconfinedBoundaryLiftsEnforcement(t *testing.T) {
	b := UnconfinedBoundary()
	if b.Enforce {
		t.Fatalf("unconfined boundary should disable path enforcement: %+v", b)
	}
	if !b.AllowMutations {
		t.Fatalf("unconfined boundary should allow mutations: %+v", b)
	}
}

func TestBoundaryGuardsAreHardDenyOnly(t *testing.T) {
	want := errors.New("denied")
	b := WorkspaceBoundary{Enforce: true, AllowMutations: true, Guards: []Guard{nil, testGuard{err: want}}}
	err := b.Check(ToolInfo{Name: "bash", Kind: ToolKindShell}, providers.ToolCall{Name: "bash"})
	if !errors.Is(err, want) {
		t.Fatalf("boundary should return guard denial, got %v", err)
	}
}
