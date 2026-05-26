package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

const browserBridgeMaxResponseBytes = 20 * 1024 * 1024

type BrowserTool struct {
	env    *Env
	client *http.Client
}

type browserToolArgs struct {
	Action     string   `json:"action"`
	TargetID   string   `json:"target_id"`
	URL        string   `json:"url"`
	Selector   string   `json:"selector"`
	Format     string   `json:"format"`
	FullPage   bool     `json:"full_page"`
	Quality    int      `json:"quality"`
	Enhanced   bool     `json:"enhanced"`
	Level      string   `json:"level"`
	Search     string   `json:"search"`
	Limit      int      `json:"limit"`
	Clear      bool     `json:"clear"`
	Failed     bool     `json:"failed"`
	Expression string   `json:"expression"`
	X          *float64 `json:"x"`
	Y          *float64 `json:"y"`
	Button     string   `json:"button"`
	ClickCount int      `json:"click_count"`
	Text       string   `json:"text"`
	Direction  string   `json:"direction"`
	Amount     int      `json:"amount"`
	Background *bool    `json:"background"`
	WindowID   *int     `json:"window_id"`
}

func NewBrowserTool(env *Env) *BrowserTool {
	return &BrowserTool{
		env: env,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (t *BrowserTool) Name() string { return "browser" }

func (t *BrowserTool) IsReadOnly() bool { return false }

func (t *BrowserTool) IsConcurrencySafe() bool { return false }

func (t *BrowserTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "browser",
		Description: "Inspect and operate real Wuu Browser tabs through the local Browser Bridge. Use this for browser-backed validation: list tabs, open or navigate pages, read screenshots/DOM/console/network, and interact with the active page.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type": "string",
					"enum": []string{
						"list_tabs", "active_tab", "open_tab", "navigate", "activate",
						"reload", "back", "forward", "close", "screenshot", "content",
						"snapshot", "dom", "console", "network", "evaluate", "click",
						"type", "scroll",
					},
					"description": "Browser action to run.",
				},
				"target_id":   map[string]any{"type": "string", "description": "CDP target id returned by list_tabs or active_tab. Required for target-specific actions."},
				"url":         map[string]any{"type": "string", "description": "Absolute URL for open_tab or navigate."},
				"selector":    map[string]any{"type": "string", "description": "Optional CSS selector for content or dom."},
				"format":      map[string]any{"type": "string", "enum": []string{"png", "jpeg"}, "description": "Screenshot format."},
				"full_page":   map[string]any{"type": "boolean", "description": "Capture a full page screenshot."},
				"quality":     map[string]any{"type": "integer", "description": "JPEG screenshot quality, 1-100."},
				"enhanced":    map[string]any{"type": "boolean", "description": "Use enhanced accessibility snapshot."},
				"level":       map[string]any{"type": "string", "enum": []string{"error", "warning", "info", "debug"}, "description": "Console level filter."},
				"search":      map[string]any{"type": "string", "description": "Search text for console or network entries."},
				"limit":       map[string]any{"type": "integer", "description": "Maximum console or network entries to return."},
				"clear":       map[string]any{"type": "boolean", "description": "Clear console/network entries after reading, or clear text before typing."},
				"failed":      map[string]any{"type": "boolean", "description": "For network, return failed requests only."},
				"expression":  map[string]any{"type": "string", "description": "JavaScript expression for evaluate."},
				"x":           map[string]any{"type": "number", "description": "Viewport x coordinate for click or type."},
				"y":           map[string]any{"type": "number", "description": "Viewport y coordinate for click or type."},
				"button":      map[string]any{"type": "string", "enum": []string{"left", "right", "middle"}, "description": "Mouse button for click."},
				"click_count": map[string]any{"type": "integer", "description": "Click count for click."},
				"text":        map[string]any{"type": "string", "description": "Text to type."},
				"direction":   map[string]any{"type": "string", "enum": []string{"up", "down", "left", "right"}, "description": "Scroll direction."},
				"amount":      map[string]any{"type": "integer", "description": "Scroll amount."},
				"background":  map[string]any{"type": "boolean", "description": "Open the new tab in the background."},
				"window_id":   map[string]any{"type": "integer", "description": "Optional browser window id for open_tab."},
			},
			"required": []string{"action"},
		},
	}
}

func (t *BrowserTool) Classify(argsJSON string) ToolClassification {
	var args browserToolArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return ToolClassification{
			ReadOnly:        false,
			ConcurrencySafe: false,
			Risk:            ToolRiskHigh,
			Reason:          "invalid browser arguments",
		}
	}
	if browserActionIsReadOnly(args.Action) {
		return ToolClassification{
			ReadOnly:        true,
			ConcurrencySafe: true,
			Risk:            ToolRiskMedium,
			Reason:          "browser inspection",
		}
	}
	return ToolClassification{
		ReadOnly:        false,
		ConcurrencySafe: false,
		Risk:            ToolRiskHigh,
		Reason:          "browser page operation",
	}
}

func (t *BrowserTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args browserToolArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}

	base, err := parseBrowserBridgeURL(t.env.BrowserBridgeURL)
	if err != nil {
		return "", err
	}

	req, err := t.buildRequest(ctx, base, args)
	if err != nil {
		return "", err
	}
	origin := base.Scheme + "://" + base.Host
	req.Header.Set("Origin", origin)
	if req.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("browser bridge request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, browserBridgeMaxResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("read browser bridge response: %w", err)
	}
	if len(body) > browserBridgeMaxResponseBytes {
		return "", fmt.Errorf("browser bridge response exceeded %d bytes", browserBridgeMaxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("browser bridge returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload any
	if len(strings.TrimSpace(string(body))) == 0 {
		payload = map[string]any{"ok": true}
	} else if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("parse browser bridge response: %w", err)
	}

	return mustJSON(map[string]any{
		"action": args.Action,
		"result": payload,
	})
}

func (t *BrowserTool) buildRequest(ctx context.Context, base *url.URL, args browserToolArgs) (*http.Request, error) {
	action := strings.TrimSpace(args.Action)
	switch action {
	case "list_tabs":
		return browserRequest(ctx, http.MethodGet, base, "/tabs", nil, nil)
	case "active_tab":
		return browserRequest(ctx, http.MethodGet, base, "/active-tab", nil, nil)
	case "open_tab":
		if strings.TrimSpace(args.URL) == "" {
			return nil, errors.New("url is required for open_tab")
		}
		body := map[string]any{"url": args.URL}
		if args.Background != nil {
			body["background"] = *args.Background
		}
		if args.WindowID != nil {
			body["windowId"] = *args.WindowID
		}
		return browserRequest(ctx, http.MethodPost, base, "/tabs", nil, body)
	case "navigate":
		if strings.TrimSpace(args.URL) == "" {
			return nil, errors.New("url is required for navigate")
		}
		targetPath, err := targetActionPath(args.TargetID, "navigate")
		if err != nil {
			return nil, err
		}
		return browserRequest(ctx, http.MethodPost, base, targetPath, nil, map[string]any{"url": args.URL})
	case "activate", "reload", "back", "forward":
		targetPath, err := targetActionPath(args.TargetID, action)
		if err != nil {
			return nil, err
		}
		return browserRequest(ctx, http.MethodPost, base, targetPath, nil, map[string]any{})
	case "close":
		targetPath, err := targetPath(args.TargetID)
		if err != nil {
			return nil, err
		}
		return browserRequest(ctx, http.MethodDelete, base, targetPath, nil, nil)
	case "screenshot":
		targetPath, err := targetActionPath(args.TargetID, "screenshot")
		if err != nil {
			return nil, err
		}
		query := url.Values{}
		if args.Format != "" {
			query.Set("format", args.Format)
		}
		if args.FullPage {
			query.Set("fullPage", "1")
		}
		if args.Quality > 0 {
			query.Set("quality", strconv.Itoa(args.Quality))
		}
		return browserRequest(ctx, http.MethodGet, base, targetPath, query, nil)
	case "content", "dom":
		targetPath, err := targetActionPath(args.TargetID, action)
		if err != nil {
			return nil, err
		}
		query := url.Values{}
		if args.Selector != "" {
			query.Set("selector", args.Selector)
		}
		return browserRequest(ctx, http.MethodGet, base, targetPath, query, nil)
	case "snapshot":
		targetPath, err := targetActionPath(args.TargetID, "snapshot")
		if err != nil {
			return nil, err
		}
		query := url.Values{}
		if args.Enhanced {
			query.Set("enhanced", "1")
		}
		return browserRequest(ctx, http.MethodGet, base, targetPath, query, nil)
	case "console":
		targetPath, err := targetActionPath(args.TargetID, "console")
		if err != nil {
			return nil, err
		}
		query := browserLogQuery(args)
		return browserRequest(ctx, http.MethodGet, base, targetPath, query, nil)
	case "network":
		targetPath, err := targetActionPath(args.TargetID, "network")
		if err != nil {
			return nil, err
		}
		query := browserLogQuery(args)
		if args.Failed {
			query.Set("failed", "1")
		}
		return browserRequest(ctx, http.MethodGet, base, targetPath, query, nil)
	case "evaluate":
		if strings.TrimSpace(args.Expression) == "" {
			return nil, errors.New("expression is required for evaluate")
		}
		targetPath, err := targetActionPath(args.TargetID, "evaluate")
		if err != nil {
			return nil, err
		}
		return browserRequest(ctx, http.MethodPost, base, targetPath, nil, map[string]any{"expression": args.Expression})
	case "click":
		point, err := browserPoint(args)
		if err != nil {
			return nil, err
		}
		targetPath, err := targetActionPath(args.TargetID, "click")
		if err != nil {
			return nil, err
		}
		body := point
		if args.Button != "" {
			body["button"] = args.Button
		}
		if args.ClickCount > 0 {
			body["clickCount"] = args.ClickCount
		}
		return browserRequest(ctx, http.MethodPost, base, targetPath, nil, body)
	case "type":
		point, err := browserPoint(args)
		if err != nil {
			return nil, err
		}
		targetPath, err := targetActionPath(args.TargetID, "type")
		if err != nil {
			return nil, err
		}
		point["text"] = args.Text
		point["clear"] = args.Clear
		return browserRequest(ctx, http.MethodPost, base, targetPath, nil, point)
	case "scroll":
		if args.Direction == "" {
			return nil, errors.New("direction is required for scroll")
		}
		targetPath, err := targetActionPath(args.TargetID, "scroll")
		if err != nil {
			return nil, err
		}
		body := map[string]any{"direction": args.Direction}
		if args.Amount > 0 {
			body["amount"] = args.Amount
		}
		return browserRequest(ctx, http.MethodPost, base, targetPath, nil, body)
	default:
		return nil, fmt.Errorf("unsupported browser action %q", args.Action)
	}
}

func browserRequest(ctx context.Context, method string, base *url.URL, suffix string, query url.Values, body any) (*http.Request, error) {
	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + suffix
	u.RawQuery = query.Encode()

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	return http.NewRequestWithContext(ctx, method, u.String(), reader)
}

func parseBrowserBridgeURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("browser bridge is unavailable outside Wuu Browser")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse browser bridge url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("browser bridge url must use http or https, got %q", u.Scheme)
	}
	if !isLoopbackHost(u.Hostname()) {
		return nil, fmt.Errorf("browser bridge url must be loopback, got %q", u.Hostname())
	}
	return u, nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func targetPath(targetID string) (string, error) {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return "", errors.New("target_id is required")
	}
	return path.Join("/tabs", url.PathEscape(targetID)), nil
}

func targetActionPath(targetID, action string) (string, error) {
	base, err := targetPath(targetID)
	if err != nil {
		return "", err
	}
	return path.Join(base, action), nil
}

func browserActionIsReadOnly(action string) bool {
	switch strings.TrimSpace(action) {
	case "list_tabs", "active_tab", "screenshot", "content", "snapshot", "dom", "console", "network":
		return true
	default:
		return false
	}
}

func browserLogQuery(args browserToolArgs) url.Values {
	query := url.Values{}
	if args.Level != "" {
		query.Set("level", args.Level)
	}
	if args.Search != "" {
		query.Set("search", args.Search)
	}
	if args.Limit > 0 {
		query.Set("limit", strconv.Itoa(args.Limit))
	}
	if args.Clear {
		query.Set("clear", "1")
	}
	return query
}

func browserPoint(args browserToolArgs) (map[string]any, error) {
	if args.X == nil || args.Y == nil {
		return nil, errors.New("x and y are required")
	}
	return map[string]any{
		"x": *args.X,
		"y": *args.Y,
	}, nil
}
