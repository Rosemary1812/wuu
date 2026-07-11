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

	"github.com/blueberrycongee/wuu/internal/activity"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

type MCPActivityBinding struct {
	Kind     activity.Kind
	PluginID string
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
		if updated, updateErr := t.activityRegistry.Update(threadID, session.ID, activity.UpdateOptions{Target: target}); updateErr == nil {
			session = updated
		}
	}

	result, callErr := t.executeKnownToolResult(ctx, call, tool)
	// Re-check the lease before publishing success. A takeover that happened
	// while the helper was running invalidates the result and prevents the
	// agent from chaining another action from stale UI state.
	if controlErr := t.activityRegistry.CheckControl(threadID, session.ID, lease.Token); controlErr != nil {
		return toolresult.Result{}, controlErr
	}
	state := activity.StateActive
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
