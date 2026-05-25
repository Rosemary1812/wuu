package insight

import (
	"fmt"
	"strings"
	"time"
)

// UsageReport is a deterministic local summary built from session logs only.
type UsageReport struct {
	Stats       AggregatedData
	Sessions    []SessionMeta
	GeneratedAt time.Time
}

// BuildUsageReport scans session logs and aggregates local usage statistics.
func BuildUsageReport(sessDir string, maxSessions int) (*UsageReport, error) {
	metas, err := ScanSessions(sessDir, maxSessions)
	if err != nil {
		return nil, err
	}
	return &UsageReport{
		Stats:       Aggregate(metas, nil),
		Sessions:    metas,
		GeneratedAt: time.Now(),
	}, nil
}

// FormatUsageReport renders local usage stats for text-based clients.
func FormatUsageReport(r *UsageReport) string {
	if r == nil || r.Stats.TotalSessions == 0 {
		return "Usage\n\nNo substantive local sessions found yet."
	}

	stats := r.Stats
	var b strings.Builder
	b.WriteString("Usage\n\n")
	fmt.Fprintf(&b, "Generated: %s\n", r.GeneratedAt.Format("2006-01-02 15:04"))
	if stats.DateRange[0] != "" || stats.DateRange[1] != "" {
		fmt.Fprintf(&b, "Date range: %s to %s\n", stats.DateRange[0], stats.DateRange[1])
	}
	fmt.Fprintf(&b, "Sessions: %d\n", stats.TotalSessions)
	fmt.Fprintf(&b, "Messages: %d\n", stats.TotalMessages)
	fmt.Fprintf(&b, "Active days: %d\n", stats.DaysActive)
	fmt.Fprintf(&b, "Total duration: %.1f hours\n", stats.TotalDurationH)
	fmt.Fprintf(&b, "Messages/day: %.1f\n", stats.MessagesPerDay)
	if stats.TotalInputTokens > 0 || stats.TotalOutputTokens > 0 {
		fmt.Fprintf(&b, "Tokens: %s input / %s output\n", compactCount(stats.TotalInputTokens), compactCount(stats.TotalOutputTokens))
	} else if stats.TotalEstTokens > 0 {
		fmt.Fprintf(&b, "Estimated tokens: %s\n", compactCount(stats.TotalEstTokens))
	}
	if stats.TotalLinesAdded > 0 || stats.TotalLinesRemoved > 0 || stats.TotalFilesModified > 0 {
		fmt.Fprintf(&b, "Code changes: +%d / -%d across %d files\n", stats.TotalLinesAdded, stats.TotalLinesRemoved, stats.TotalFilesModified)
	}

	writeUsageMap(&b, "\nTop tools", stats.ToolCounts, 8)
	writeUsageMap(&b, "\nLanguages", stats.Languages, 8)
	writeUsageHours(&b, stats.MessageHours)
	writeRecentSessions(&b, r.Sessions, 5)

	return strings.TrimRight(b.String(), "\n")
}

func writeUsageMap(b *strings.Builder, title string, values map[string]int, limit int) {
	if len(values) == 0 {
		return
	}
	b.WriteString(title)
	b.WriteString(":\n")
	for _, item := range topN(values, limit) {
		fmt.Fprintf(b, "  %s: %d\n", item.key, item.val)
	}
}

func writeUsageHours(b *strings.Builder, hours []int) {
	if len(hours) == 0 {
		return
	}
	counts := make(map[string]int)
	for _, h := range hours {
		if h >= 0 && h < 24 {
			counts[fmt.Sprintf("%02d:00", h)]++
		}
	}
	writeUsageMap(b, "\nPeak hours", counts, 5)
}

func writeRecentSessions(b *strings.Builder, sessions []SessionMeta, limit int) {
	if len(sessions) == 0 {
		return
	}
	if len(sessions) < limit {
		limit = len(sessions)
	}
	b.WriteString("\nRecent sessions:\n")
	for _, s := range sessions[:limit] {
		title := s.FirstUserMsg
		if title == "" {
			title = s.ID
		}
		fmt.Fprintf(b, "  %s  %s  %d messages  %.1f min\n",
			s.CreatedAt.Format("2006-01-02"),
			truncateStr(title, 64),
			s.UserMessages+s.AssistantMsgs,
			s.Duration.Minutes(),
		)
	}
}

func compactCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fm", float64(n)/1000000)
}
