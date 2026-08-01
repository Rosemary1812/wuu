package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/channels"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

const channelAgentInsightWindowDays = 7

type channelAgentInsightsCacheEntry struct {
	response  ChannelAgentInsightsResult
	expiresAt time.Time
}

type insightDiffLine struct {
	Op string `json:"op"`
}

type insightDiffHunk struct {
	Lines []insightDiffLine `json:"lines"`
}

type insightDiff struct {
	Hunks    []insightDiffHunk `json:"hunks"`
	NewFile  bool              `json:"new_file"`
	Lines    int               `json:"lines"`
	OldLines int               `json:"old_lines"`
	NewLines int               `json:"new_lines"`
}

type insightEditedFile struct {
	Path     string      `json:"path"`
	MovePath string      `json:"move_path"`
	Diff     insightDiff `json:"diff"`
}

type insightToolDetail struct {
	Path  string              `json:"path"`
	Diff  insightDiff         `json:"diff"`
	Files []insightEditedFile `json:"files"`
}

func (s *Server) invalidateChannelAgentInsights() {
	if s == nil {
		return
	}
	s.channelAgentInsightsMu.Lock()
	s.channelAgentInsightsCache = nil
	s.channelAgentInsightsMu.Unlock()
}

func (s *Server) handleChannelAgentInsights(ctx context.Context, req Request) error {
	if s == nil || s.channelService == nil || s.rt == nil {
		return s.writeResponse(req.ID, nil, errors.New("channels service is unavailable"))
	}
	now := time.Now().UTC()
	s.channelAgentInsightsMu.Lock()
	defer s.channelAgentInsightsMu.Unlock()
	if cached := s.channelAgentInsightsCache; cached != nil && now.Before(cached.expiresAt) {
		return s.writeResponse(req.ID, cached.response, nil)
	}
	agents, err := s.channelService.ListNamedAgents(ctx)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	insights := make([]ChannelAgentInsight, 0, len(agents))
	for _, namedAgent := range agents {
		insights = append(insights, collectChannelAgentInsight(s.rt.SessionDir, namedAgent, now))
	}
	response := ChannelAgentInsightsResult{
		GeneratedAt: now.Format(time.RFC3339Nano),
		Insights:    insights,
	}
	s.channelAgentInsightsCache = &channelAgentInsightsCacheEntry{response: response, expiresAt: now.Add(5 * time.Minute)}
	return s.writeResponse(req.ID, response, nil)
}

func collectChannelAgentInsight(sessDir string, namedAgent channels.NamedAgent, now time.Time) ChannelAgentInsight {
	result := ChannelAgentInsight{
		AgentID: namedAgent.ID, WindowDays: channelAgentInsightWindowDays,
		Languages: []ChannelAgentLanguageUsage{}, AttributionPartial: true,
	}
	metadata, found, err := session.Find(sessDir, namedAgentSessionID(namedAgent))
	if err != nil || !found {
		return result
	}
	if !metadata.UpdatedAt.IsZero() {
		result.LastActiveAt = metadata.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	if cwd := strings.TrimSpace(metadata.CWD); cwd != "" {
		result.Workspace = filepath.Base(filepath.Clean(cwd))
	}
	records, err := session.LoadHistoryRecords(sessDir, metadata.ID, true)
	if err != nil {
		return result
	}
	cutoff := now.AddDate(0, 0, -channelAgentInsightWindowDays)
	files := make(map[string]struct{})
	languageLines := make(map[string]int)
	for _, record := range records {
		if record.At.IsZero() || record.At.Before(cutoff) || record.At.After(now.Add(time.Minute)) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(record.Role), "meta") && strings.TrimSpace(record.Content) == "token_usage" {
			result.InputTokens += record.InputTokens + record.CacheReadTokens
			result.OutputTokens += record.OutputTokens
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(record.Role), "tool") {
			continue
		}
		name := strings.TrimSpace(record.Name)
		if name != "apply_patch" && name != "edit_file" && name != "write_file" {
			continue
		}
		for _, edit := range attributableEdits(record.ToolResult) {
			path := strings.TrimSpace(edit.Path)
			if strings.TrimSpace(edit.MovePath) != "" {
				path = strings.TrimSpace(edit.MovePath)
			}
			if path == "" {
				continue
			}
			additions, deletions := summarizeInsightDiff(edit.Diff)
			files[path] = struct{}{}
			result.Additions += additions
			result.Deletions += deletions
			languageLines[languageForPath(path)] += additions + deletions
		}
	}
	result.FilesChanged = len(files)
	result.Languages = buildAgentLanguageUsage(languageLines)
	return result
}

func attributableEdits(raw json.RawMessage) []insightEditedFile {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var envelope toolresult.Result
	if err := json.Unmarshal(trimmed, &envelope); err != nil || envelope.IsError {
		return nil
	}
	detailRaw := bytes.TrimSpace(envelope.StructuredContent)
	if len(detailRaw) == 0 {
		for _, part := range envelope.Content {
			candidate := bytes.TrimSpace([]byte(part.Text))
			if json.Valid(candidate) {
				detailRaw = candidate
				break
			}
		}
	}
	if len(detailRaw) == 0 {
		return nil
	}
	var detail insightToolDetail
	if err := json.Unmarshal(detailRaw, &detail); err != nil {
		return nil
	}
	if len(detail.Files) > 0 {
		return detail.Files
	}
	if strings.TrimSpace(detail.Path) != "" {
		return []insightEditedFile{{Path: detail.Path, Diff: detail.Diff}}
	}
	return nil
}

func summarizeInsightDiff(diff insightDiff) (int, int) {
	if diff.NewFile {
		return max(diff.Lines, diff.NewLines), 0
	}
	additions, deletions := 0, 0
	for _, hunk := range diff.Hunks {
		for _, line := range hunk.Lines {
			switch line.Op {
			case "insert":
				additions++
			case "delete":
				deletions++
			}
		}
	}
	if additions == 0 && deletions == 0 {
		return diff.NewLines, diff.OldLines
	}
	return additions, deletions
}

func buildAgentLanguageUsage(lines map[string]int) []ChannelAgentLanguageUsage {
	total := 0
	for _, count := range lines {
		total += count
	}
	if total == 0 {
		return []ChannelAgentLanguageUsage{}
	}
	out := make([]ChannelAgentLanguageUsage, 0, len(lines))
	for name, count := range lines {
		out = append(out, ChannelAgentLanguageUsage{Name: name, Lines: count, Share: float64(count) / float64(total)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Lines != out[j].Lines {
			return out[i].Lines > out[j].Lines
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

func languageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".tsx":
		return "TypeScript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "JavaScript"
	case ".go":
		return "Go"
	case ".py":
		return "Python"
	case ".rs":
		return "Rust"
	case ".swift":
		return "Swift"
	case ".css", ".scss", ".sass", ".less":
		return "CSS"
	case ".html", ".htm":
		return "HTML"
	case ".json", ".jsonc":
		return "JSON"
	case ".md", ".mdx":
		return "Markdown"
	case ".sh", ".bash", ".zsh":
		return "Shell"
	case ".sql":
		return "SQL"
	case ".java", ".kt", ".kts":
		return "JVM"
	case ".c", ".h", ".cc", ".cpp", ".hpp":
		return "C/C++"
	default:
		return "Other"
	}
}
