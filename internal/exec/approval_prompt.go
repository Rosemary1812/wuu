package exec

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/blueberrycongee/wuu/internal/appserver"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// ttyApprovalPrompter answers approval requests by asking the human on
// the process's controlling terminal. exec is non-interactive in the
// protocol sense (no TUI), but a run started from a shell still has a
// terminal attached - and the person who started it is exactly the
// reviewer the "user" approvals policy asks for.
type ttyApprovalPrompter struct {
	mu     sync.Mutex
	tty    io.Writer
	reader *bufio.Reader
}

// newTTYApprovalPrompter returns nil when no controlling terminal is
// available (CI, cron, pipes); callers then fall through to the
// deny-with-reason path.
func newTTYApprovalPrompter() *ttyApprovalPrompter {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil
	}
	return &ttyApprovalPrompter{tty: tty, reader: bufio.NewReader(tty)}
}

// prompt asks one approval question and reports the user's decision.
// ok=false means the terminal could not deliver a decision (EOF, write
// error) and the caller should fall back to denying.
func (p *ttyApprovalPrompter) prompt(request appserver.ToolApprovalRequest) (appserver.ToolApprovalResponse, bool) {
	if p == nil || p.tty == nil {
		return appserver.ToolApprovalResponse{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, err := io.WriteString(p.tty, renderApprovalPrompt(request)); err != nil {
		return appserver.ToolApprovalResponse{}, false
	}
	line, err := p.reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return appserver.ToolApprovalResponse{}, false
	}
	return approvalResponseForAnswer(line), true
}

func renderApprovalPrompt(request appserver.ToolApprovalRequest) string {
	var b strings.Builder
	b.WriteString("\nwuu: approval required\n")
	fmt.Fprintf(&b, "  tool: %s", request.ToolName)
	if request.Risk != "" {
		fmt.Fprintf(&b, " (risk=%s)", request.Risk)
	}
	b.WriteString("\n")
	if preview := strings.TrimSpace(request.ArgumentsPreview); preview != "" {
		fmt.Fprintf(&b, "  args: %s\n", preview)
	}
	if reason := strings.TrimSpace(request.PolicyReason); reason != "" {
		fmt.Fprintf(&b, "  reason: %s\n", reason)
	}
	b.WriteString("approve? [y]es once / [a]lways this session / [N]o: ")
	return b.String()
}

func approvalResponseForAnswer(answer string) appserver.ToolApprovalResponse {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return appserver.ToolApprovalResponse{
			Decision: string(tools.ToolApprovalDecisionApproved),
			Reason:   "approved by the user at the terminal",
		}
	case "a", "always":
		return appserver.ToolApprovalResponse{
			Decision: string(tools.ToolApprovalDecisionApprovedForSession),
			Reason:   "approved for this session by the user at the terminal",
		}
	default:
		return appserver.ToolApprovalResponse{
			Decision: string(tools.ToolApprovalDecisionDenied),
			Reason:   "denied by the user at the terminal",
		}
	}
}
