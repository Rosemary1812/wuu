package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/activity"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

type MCPActivityBinding struct {
	Kind     activity.Kind
	PluginID string
}

type cuaSequenceRequest struct {
	Action           string           `json:"action"`
	App              string           `json:"app"`
	ForegroundPolicy string           `json:"foreground_policy"`
	Steps            []map[string]any `json:"steps"`
}

func (t *Toolkit) SetActivityRegistry(registry *activity.Registry) {
	if t == nil {
		return
	}
	t.activityRegistry = registry
}

func (t *Toolkit) SetMCPActivityBindings(bindings map[string]MCPActivityBinding) {
	if t == nil {
		return
	}
	t.mcpActivityBindings = cloneMCPActivityBindings(bindings)
}

func cloneMCPActivityBindings(bindings map[string]MCPActivityBinding) map[string]MCPActivityBinding {
	if len(bindings) == 0 {
		return nil
	}
	out := make(map[string]MCPActivityBinding, len(bindings))
	for server, binding := range bindings {
		server = strings.TrimSpace(server)
		binding.PluginID = strings.TrimSpace(binding.PluginID)
		if server != "" && binding.PluginID != "" && binding.Kind != "" {
			out[server] = binding
		}
	}
	return out
}

func (t *Toolkit) executeActivityBoundToolResult(ctx context.Context, call providers.ToolCall, tool Tool, serverName string) (toolresult.Result, error) {
	binding, bound := t.mcpActivityBindings[strings.TrimSpace(serverName)]
	if !bound {
		return t.executeKnownToolResult(ctx, call, tool)
	}
	if t.activityRegistry == nil {
		return toolresult.Result{}, errors.New("activity registry is unavailable")
	}
	threadID := ""
	workdir := ""
	if t.env != nil {
		threadID = strings.TrimSpace(t.env.SessionID)
		workdir = strings.TrimSpace(t.env.RootDir)
	}
	if threadID == "" {
		return toolresult.Result{}, errors.New("activity-bound MCP tool requires a thread context")
	}
	target := mcpActivityTarget(call.Arguments)
	if binding.Kind == activity.KindCUA {
		target = t.canonicalCUATarget(ctx, tool, target)
	}
	session, lease, err := t.activityRegistry.Acquire(activity.StartOptions{
		Kind:     binding.Kind,
		ThreadID: threadID,
		Workdir:  workdir,
		PluginID: binding.PluginID,
		Target:   target,
	})
	if err != nil {
		return toolresult.Result{}, err
	}
	if err := t.activityRegistry.CheckControl(threadID, session.ID, lease.Token); err != nil {
		return toolresult.Result{}, err
	}
	if target != "" && target != session.Target {
		updated, updateErr := t.activityRegistry.Update(threadID, session.ID, activity.UpdateOptions{Target: target})
		if updateErr != nil {
			return toolresult.Result{}, updateErr
		}
		session = updated
	}

	sequence, isSequence := parseCUASequence(call.Arguments)
	var result toolresult.Result
	var callErr error
	sequenceStatus := ""
	if isSequence {
		result, sequenceStatus, callErr = t.executeCUASequence(ctx, tool, threadID, session.ID, lease.Token, sequence)
	} else {
		result, callErr = t.executeKnownToolResult(ctx, call, tool)
	}
	// Re-check the lease before publishing success. A takeover that happened
	// while the helper was running invalidates the result and prevents the
	// agent from chaining another action from stale UI state.
	if controlErr := t.activityRegistry.CheckControl(threadID, session.ID, lease.Token); controlErr != nil {
		return toolresult.Result{}, controlErr
	}
	state := cuaActivityControlState(call.Arguments, result)
	if sequenceStatus == "policy_paused" {
		state = activity.StateWaitingConfirmation
	}
	update := activity.UpdateOptions{State: state}
	if callErr == nil {
		previewURI, previewErr := t.persistActivityPreview(session.ID, result)
		if previewErr != nil {
			callErr = previewErr
		} else {
			update.Preview = previewURI
		}
	}
	if callErr != nil {
		update.State = activity.StateError
		update.Error = callErr.Error()
	} else {
		update.ClearError = true
		update.Interaction = cuaActivityInteraction(result)
	}
	if updated, updateErr := t.activityRegistry.Update(threadID, session.ID, update); updateErr == nil {
		session = updated
	} else if callErr == nil {
		callErr = fmt.Errorf("update activity: %w", updateErr)
	}
	result.Activity = &toolresult.ActivityRef{
		ID:         session.ID,
		Kind:       string(session.Kind),
		State:      string(session.State),
		ThreadID:   session.ThreadID,
		PreviewURI: session.Preview,
	}
	return result, callErr
}

func (t *Toolkit) canonicalCUATarget(ctx context.Context, tool Tool, target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return target
	}
	arguments, _ := json.Marshal(map[string]any{"action": "list_apps", "app": target})
	result, err := t.executeKnownToolResult(ctx, providers.ToolCall{Name: tool.Name(), Arguments: string(arguments)}, tool)
	if err != nil || result.IsError {
		return target
	}
	return canonicalCUATargetFromApps(target, result.StructuredContent)
}

func canonicalCUATargetFromApps(target string, structured json.RawMessage) string {
	var payload struct {
		ResolvedApp *struct {
			ID               string `json:"id"`
			BundleIdentifier string `json:"bundleIdentifier"`
			Path             string `json:"path"`
		} `json:"resolved_app"`
		Apps []struct {
			ID               string `json:"id"`
			DisplayName      string `json:"displayName"`
			BundleIdentifier string `json:"bundleIdentifier"`
			Path             string `json:"path"`
		} `json:"apps"`
	}
	if json.Unmarshal(structured, &payload) != nil {
		return strings.TrimSpace(target)
	}
	if payload.ResolvedApp != nil {
		if canonical := strings.TrimSpace(payload.ResolvedApp.BundleIdentifier); canonical != "" {
			return canonical
		}
		if canonical := strings.TrimSpace(payload.ResolvedApp.Path); canonical != "" {
			return canonical
		}
		if canonical := strings.TrimSpace(payload.ResolvedApp.ID); canonical != "" {
			return canonical
		}
	}
	normalized := strings.ToLower(strings.TrimSpace(target))
	for _, app := range payload.Apps {
		if normalized != strings.ToLower(strings.TrimSpace(app.ID)) &&
			normalized != strings.ToLower(strings.TrimSpace(app.DisplayName)) &&
			normalized != strings.ToLower(strings.TrimSpace(app.BundleIdentifier)) &&
			normalized != strings.ToLower(strings.TrimSpace(app.Path)) {
			continue
		}
		if canonical := strings.TrimSpace(app.BundleIdentifier); canonical != "" {
			return canonical
		}
		if canonical := strings.TrimSpace(app.Path); canonical != "" {
			return canonical
		}
		if canonical := strings.TrimSpace(app.ID); canonical != "" {
			return canonical
		}
	}
	return strings.TrimSpace(target)
}

func cuaActivityInteraction(result toolresult.Result) *activity.Interaction {
	var structured struct {
		Interaction *activity.Interaction `json:"interaction"`
	}
	if json.Unmarshal(result.StructuredContent, &structured) != nil || structured.Interaction == nil {
		return nil
	}
	interaction := structured.Interaction
	if interaction.Kind != "click" && interaction.Kind != "drag" && interaction.Kind != "scroll" && interaction.Kind != "type" {
		return nil
	}
	if interaction.X < 0 || interaction.X > 1 || interaction.Y < 0 || interaction.Y > 1 {
		return nil
	}
	if (interaction.ToX != nil && (*interaction.ToX < 0 || *interaction.ToX > 1)) ||
		(interaction.ToY != nil && (*interaction.ToY < 0 || *interaction.ToY > 1)) {
		return nil
	}
	interaction.Revision = time.Now().UnixNano()
	return interaction
}

func cuaActivityControlState(arguments string, result toolresult.Result) activity.State {
	var input map[string]any
	_ = json.Unmarshal([]byte(arguments), &input)
	policy, _ := input["foreground_policy"].(string)
	// require forcibly activates the target up front (a visible takeover) regardless
	// of which per-action level ultimately runs, so it is always foreground.
	if policy == "require" {
		return activity.StateForegroundControlled
	}
	var structured map[string]any
	if json.Unmarshal(result.StructuredContent, &structured) == nil {
		// A visible focus change means the foreground moved, whatever the mechanism.
		if changed, _ := structured["foreground_changed"].(bool); changed {
			return activity.StateForegroundControlled
		}
		switch mechanism, _ := structured["mechanism"].(string); mechanism {
		case "foreground_native":
			return activity.StateForegroundControlled
		case "background_ax", "background_directed":
			// Directed input (CGEvent.postToPid) reaches the target process without
			// activating it, so it is a background-controlled state like plain AX.
			return activity.StateBackgroundControlled
		}
	}
	// No mechanism reported (observe, permission checks): allow alone does not take
	// the foreground, so the action stayed in the background.
	return activity.StateBackgroundControlled
}

func parseCUASequence(arguments string) (cuaSequenceRequest, bool) {
	var request cuaSequenceRequest
	if json.Unmarshal([]byte(arguments), &request) != nil || request.Action != "sequence" {
		return cuaSequenceRequest{}, false
	}
	return request, true
}

func (t *Toolkit) executeCUASequence(ctx context.Context, tool Tool, threadID, activityID, leaseToken string, request cuaSequenceRequest) (toolresult.Result, string, error) {
	if len(request.Steps) == 0 || len(request.Steps) > 64 {
		return toolresult.Result{}, "failed", errors.New("CUA sequence requires 1 to 64 steps")
	}
	completed := make([]map[string]any, 0, len(request.Steps))
	var lastImage *toolresult.ContentPart
	// Track the most-disruptive control level any step actually used so the whole
	// sequence maps to an honest activity state (a background sequence must not look
	// foreground just because foreground_policy was allowed).
	control := sequenceControl{}
	for index, source := range request.Steps {
		if err := t.activityRegistry.CheckControl(threadID, activityID, leaseToken); err != nil {
			return cuaSequenceResult("control_revoked", completed, index, lastImage, control), "control_revoked", err
		}
		step := make(map[string]any, len(source)+2)
		for key, value := range source {
			step[key] = value
		}
		action, _ := step["action"].(string)
		if strings.TrimSpace(action) == "" || action == "sequence" {
			return cuaSequenceResult("failed", completed, index, lastImage, control), "failed", fmt.Errorf("CUA sequence step %d has an invalid action", index)
		}
		risk, _ := step["risk"].(string)
		if risk != "safe" && risk != "external_side_effect" && risk != "destructive" {
			return cuaSequenceResult("failed", completed, index, lastImage, control), "failed", fmt.Errorf("CUA sequence step %d must declare risk", index)
		}
		confirmed, _ := step["confirmed"].(bool)
		if risk != "safe" && !confirmed {
			return cuaSequenceResult("policy_paused", completed, index, lastImage, control), "policy_paused", nil
		}
		delete(step, "risk")
		delete(step, "confirmed")
		if _, ok := step["app"]; !ok && request.App != "" {
			step["app"] = request.App
		}
		if _, ok := step["foreground_policy"]; !ok && request.ForegroundPolicy != "" {
			step["foreground_policy"] = request.ForegroundPolicy
		}
		encoded, err := json.Marshal(step)
		if err != nil {
			return cuaSequenceResult("failed", completed, index, lastImage, control), "failed", err
		}
		stepResult, err := t.executeKnownToolResult(ctx, providers.ToolCall{Name: tool.Name(), Arguments: string(encoded)}, tool)
		control.observe(string(encoded), stepResult)
		if err != nil || stepResult.IsError {
			completed = append(completed, map[string]any{"index": index, "action": action, "status": "failed"})
			if err == nil {
				err = errors.New(stepResult.TextProjection())
			}
			return cuaSequenceResult("partial", completed, index+1, lastImage, control), "partial", err
		}
		for i := range stepResult.Content {
			if stepResult.Content[i].Type == toolresult.ContentTypeImage {
				copy := stepResult.Content[i]
				lastImage = &copy
			}
		}
		completed = append(completed, map[string]any{"index": index, "action": action, "status": "completed"})
		if interaction := cuaActivityInteraction(stepResult); interaction != nil {
			if controlErr := t.activityRegistry.CheckControl(threadID, activityID, leaseToken); controlErr != nil {
				return cuaSequenceResult("control_revoked", completed, index+1, lastImage, control), "control_revoked", controlErr
			}
			if _, updateErr := t.activityRegistry.Update(threadID, activityID, activity.UpdateOptions{Interaction: interaction}); updateErr != nil {
				return cuaSequenceResult("partial", completed, index+1, lastImage, control), "partial", updateErr
			}
		}
	}
	return cuaSequenceResult("completed", completed, len(request.Steps), lastImage, control), "completed", nil
}

// sequenceControl accumulates the highest control level reached across a CUA
// sequence's steps so the aggregate result can report an accurate mechanism.
type sequenceControl struct {
	mechanism         string
	foregroundChanged bool
}

func mechanismRank(mechanism string) int {
	switch mechanism {
	case "foreground_native":
		return 3
	case "background_directed":
		return 2
	case "background_ax":
		return 1
	default:
		return 0
	}
}

func (c *sequenceControl) observe(arguments string, result toolresult.Result) {
	var structured map[string]any
	if json.Unmarshal(result.StructuredContent, &structured) == nil {
		if mechanism, _ := structured["mechanism"].(string); mechanismRank(mechanism) > mechanismRank(c.mechanism) {
			c.mechanism = mechanism
		}
		if changed, _ := structured["foreground_changed"].(bool); changed {
			c.foregroundChanged = true
		}
	}
	// A require step force-activates the target even when its own mechanism reads as
	// background, so treat it as a foreground change for the sequence.
	var input map[string]any
	if json.Unmarshal([]byte(arguments), &input) == nil {
		if policy, _ := input["foreground_policy"].(string); policy == "require" {
			c.foregroundChanged = true
		}
	}
}

func cuaSequenceResult(status string, completed []map[string]any, nextStep int, imagePart *toolresult.ContentPart, control sequenceControl) toolresult.Result {
	payload := map[string]any{"status": status, "completed_steps": completed, "next_step": nextStep}
	if control.mechanism != "" {
		payload["mechanism"] = control.mechanism
	}
	if control.foregroundChanged {
		payload["foreground_changed"] = true
	}
	structured, _ := json.Marshal(payload)
	content := []toolresult.ContentPart{{Type: toolresult.ContentTypeText, Text: fmt.Sprintf("CUA sequence %s after %d step(s).", status, len(completed))}}
	if imagePart != nil {
		content = append(content, *imagePart)
	}
	return toolresult.Result{Content: content, StructuredContent: structured, IsError: status == "failed" || status == "partial"}
}

func (t *Toolkit) persistActivityPreview(activityID string, result toolresult.Result) (string, error) {
	if t == nil || t.env == nil || strings.TrimSpace(t.env.SessionDir) == "" {
		return "", nil
	}
	for _, part := range result.Content {
		if part.Type != toolresult.ContentTypeImage || strings.TrimSpace(part.Data) == "" {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(part.Data)
		if err != nil {
			return "", fmt.Errorf("decode activity preview: %w", err)
		}
		if len(data) > 32*1024*1024 {
			return "", fmt.Errorf("activity preview exceeds 32 MiB")
		}
		extension := ".img"
		switch strings.ToLower(strings.TrimSpace(part.MIMEType)) {
		case "image/png":
			extension = ".png"
			data = cropTransparentPNG(data)
		case "image/jpeg":
			extension = ".jpg"
		case "image/webp":
			extension = ".webp"
		}
		dir := filepath.Join(t.env.SessionDir, "activities", strings.TrimSpace(activityID))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create activity preview directory: %w", err)
		}
		path := filepath.Join(dir, "preview"+extension)
		temporary := path + ".tmp"
		if err := os.WriteFile(temporary, data, 0o600); err != nil {
			return "", fmt.Errorf("write activity preview: %w", err)
		}
		if err := os.Rename(temporary, path); err != nil {
			_ = os.Remove(temporary)
			return "", fmt.Errorf("publish activity preview: %w", err)
		}
		return (&url.URL{Scheme: "file", Path: path}).String(), nil
	}
	return "", nil
}

func cropTransparentPNG(data []byte) []byte {
	source, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return data
	}
	bounds := source.Bounds()
	visible := image.Rectangle{Min: bounds.Max, Max: bounds.Min}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := source.At(x, y).RGBA()
			if alpha < 0x8000 {
				continue
			}
			if x < visible.Min.X {
				visible.Min.X = x
			}
			if y < visible.Min.Y {
				visible.Min.Y = y
			}
			if x+1 > visible.Max.X {
				visible.Max.X = x + 1
			}
			if y+1 > visible.Max.Y {
				visible.Max.Y = y + 1
			}
		}
	}
	if visible.Empty() || visible == bounds {
		return data
	}
	cropped := image.NewNRGBA(image.Rect(0, 0, visible.Dx(), visible.Dy()))
	draw.Draw(cropped, cropped.Bounds(), source, visible.Min, draw.Src)
	var output bytes.Buffer
	if err := png.Encode(&output, cropped); err != nil {
		return data
	}
	return output.Bytes()
}

func mcpActivityTarget(arguments string) string {
	var values map[string]any
	if json.Unmarshal([]byte(arguments), &values) != nil {
		return ""
	}
	for _, key := range []string{"app", "bundle_id", "target"} {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
