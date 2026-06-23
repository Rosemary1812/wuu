package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	proc "github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/providers"
)

// ---------------------------------------------------------------------------
// report_listening_ports
// ---------------------------------------------------------------------------

// ReportListeningPortsTool lets the agent tell the GUI that a dev server it
// just started (or that was already running) is listening on one or more
// localhost ports. The desktop uses the report to surface clickable port
// chips in the workspace sidebar and to auto-open the in-app browser
// preview at the first reported URL.
type ReportListeningPortsTool struct{ env *Env }

func NewReportListeningPortsTool(env *Env) *ReportListeningPortsTool {
	return &ReportListeningPortsTool{env: env}
}

func (t *ReportListeningPortsTool) Name() string { return "report_listening_ports" }

// reportListeningPortsNoURLAccess blocks the agent from opening arbitrary
// URLs in the in-app browser. The desktop only previews the well-known
// localhost loopback.
func (t *ReportListeningPortsTool) IsReadOnly() bool        { return true }
func (t *ReportListeningPortsTool) IsConcurrencySafe() bool { return true }

func (t *ReportListeningPortsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "report_listening_ports",
		Description: "Tell the desktop GUI about one or more localhost ports that are now " +
			"listening (e.g. because you just started a dev server, ran a long-lived " +
			"process, or noticed an open port in the workspace). The desktop will surface " +
			"clickable port chips in the sidebar and auto-open the in-app browser at " +
			"the first reported URL. Use this after launching a frontend dev server " +
			"(Vite, webpack, Next.js, etc.) so the user can immediately preview the " +
			"running app without leaving the conversation. Pass the port numbers you " +
			"want surfaced; you do not need to include every open port on the host.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ports": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":    "integer",
						"minimum": 1,
						"maximum": 65535,
					},
					"minItems":    0,
					"maxItems":    16,
					"description": "TCP port numbers that the dev server is listening on, in the order the user should see them. Duplicates are de-duplicated.",
				},
				"process_id": map[string]any{
					"type":        "string",
					"description": "Optional managed process id returned by the active background process capability when available. When provided, the desktop binds these ports to that background process.",
				},
				"primary_port": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     65535,
					"description": "Optional primary port to open first. Defaults to the first valid port in ports.",
				},
			},
			"required": []string{"ports"},
		},
	}
}

type reportListeningPortsArgs struct {
	Ports       []int  `json:"ports"`
	ProcessID   string `json:"process_id"`
	PrimaryPort int    `json:"primary_port"`
}

func (t *ReportListeningPortsTool) Execute(_ context.Context, argsJSON string) (string, error) {
	trimmed := strings.TrimSpace(argsJSON)
	if trimmed == "" {
		trimmed = "{}"
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var args reportListeningPortsArgs
	if err := decoder.Decode(&args); err != nil {
		return "", fmt.Errorf("invalid tool arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return "", fmt.Errorf("invalid tool arguments: multiple JSON values")
	}

	ports := normalizeListeningPorts(args.Ports)
	previewURLs := proc.PreviewURLsFromPorts(ports)
	primaryURL := ""
	if args.PrimaryPort >= 1 && args.PrimaryPort <= 65535 {
		urls := proc.PreviewURLsFromPorts([]int{args.PrimaryPort})
		if len(urls) > 0 {
			primaryURL = urls[0]
		}
	}
	var updatedProcess *proc.Process
	processID := strings.TrimSpace(args.ProcessID)
	if processID != "" {
		manager, err := t.env.ProcessManager()
		if err != nil {
			return "", err
		}
		updated, err := manager.UpdatePreview(processID, previewURLs, primaryURL)
		if err != nil {
			return "", err
		}
		updatedProcess = updated
	}
	if t.env.OnPortsReported != nil {
		t.env.OnPortsReported(ports)
	}

	out := map[string]any{
		"action":        "report_listening_ports",
		"status":        "noted",
		"ports":         ports,
		"preview_urls":  previewURLs,
		"preview_count": len(ports),
		"hint":          "the desktop will open the in-app browser at the first reported URL",
	}
	if processID != "" {
		out["process_id"] = processID
	}
	if updatedProcess != nil {
		out["process"] = redactProcess(t.env, *updatedProcess)
	}
	return mustJSON(out)
}

// normalizeListeningPorts drops out-of-range entries and removes duplicates
// while preserving the agent-provided order. The first valid port drives the
// default browser preview.
func normalizeListeningPorts(in []int) []int {
	if len(in) == 0 {
		return []int{}
	}
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, p := range in {
		if p < 1 || p > 65535 {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
