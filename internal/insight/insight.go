package insight

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Run is the top-level orchestrator. It sends ProgressEvent updates through
// the channel and closes it when done.
func Run(ctx context.Context, cfg RunConfig, progress chan<- ProgressEvent) {
	defer close(progress)

	outputDir := strings.TrimSpace(cfg.OutputDir)
	if outputDir == "" {
		outputDir = UsageDataDir("")
		if outputDir == "" {
			sendErr(progress, fmt.Errorf("no user state directory available"))
			return
		}
	}
	cacheDir := CacheDir(outputDir)

	// Phase 1: Scan sessions.
	send(progress, "scanning", "Scanning session files...", 0.05)

	var metas []SessionMeta
	var err error
	if strings.TrimSpace(cfg.ScopeCWD) != "" {
		metas, err = ScanSessionsForCWD(cfg.SessionDir, cfg.ScopeCWD, cfg.MaxSessions)
	} else {
		metas, err = ScanSessions(cfg.SessionDir, cfg.MaxSessions)
	}
	if err != nil {
		sendErr(progress, fmt.Errorf("scan sessions: %w", err))
		return
	}
	if len(metas) == 0 {
		sendErr(progress, fmt.Errorf("no sessions found in %s", cfg.SessionDir))
		return
	}

	// Compute quick stats for the progress message.
	totalMsgs := 0
	for _, m := range metas {
		totalMsgs += m.UserMessages + m.AssistantMsgs
	}
	send(progress, "scanning",
		fmt.Sprintf("Found %d sessions (%d messages total)", len(metas), totalMsgs), 0.10)

	// Cache session metadata.
	for _, m := range metas {
		_ = SaveCachedMeta(cacheDir, m)
	}

	// Phase 2: Extract facets via LLM.
	facets := make(map[string]Facet)
	uncached := 0

	// Load cached facets first.
	for _, m := range metas {
		if cached := LoadCachedFacet(cacheDir, m.ID); cached != nil {
			facets[m.ID] = *cached
		} else {
			uncached++
		}
	}

	cachedCount := len(facets)
	if cachedCount > 0 {
		send(progress, "extracting",
			fmt.Sprintf("Loaded %d cached facets, %d remaining", cachedCount, uncached), 0.12)
	}

	if uncached > 0 {
		// Cap facet extractions to control cost.
		maxExtractions := 50
		if uncached > maxExtractions {
			uncached = maxExtractions
		}

		extracted := 0
		errCount := 0
		for _, m := range metas {
			if ctx.Err() != nil {
				send(progress, "error",
					fmt.Sprintf("Cancelled: %d extracted, %d failed", extracted-errCount, errCount), 0)
				sendErr(progress, ctx.Err())
				return
			}
			if _, ok := facets[m.ID]; ok {
				continue // already cached
			}
			if extracted >= maxExtractions {
				break
			}

			pct := 0.12 + 0.55*float64(extracted)/float64(uncached)
			preview := truncateStr(m.FirstUserMsg, 40)
			if preview == "" {
				preview = m.ID
			}
			send(progress, "extracting",
				fmt.Sprintf("Analyzing session %d/%d: \"%s\"", extracted+1, uncached, preview), pct)

			transcript, err := FormatTranscript(cfg.SessionDir, m.ID)
			if err != nil {
				send(progress, "extracting",
					fmt.Sprintf("Session %d/%d: failed to read transcript: %v", extracted+1, uncached, err), pct)
				errCount++
				extracted++
				continue
			}

			facet, err := ExtractFacet(ctx, cfg.Client, cfg.Model, m.ID, transcript)
			if err != nil {
				send(progress, "extracting",
					fmt.Sprintf("Session %d/%d: LLM error: %v", extracted+1, uncached, err), pct)
				errCount++
				extracted++
				continue // non-fatal
			}

			facets[m.ID] = facet
			_ = SaveCachedFacet(cacheDir, facet)
			extracted++
		}

		if errCount > 0 {
			send(progress, "extracting",
				fmt.Sprintf("⚠ %d/%d extractions failed (continuing with %d)", errCount, extracted, len(facets)), 0.70)
		}
	}

	// Filter warmup/minimal sessions: if the only goal category is "warmup_minimal",
	// the session had no real analytical value and should be excluded from the report.
	filteredMetas := metas[:0]
	for _, m := range metas {
		if f, ok := facets[m.ID]; ok && isWarmupOnly(f) {
			delete(facets, m.ID)
			continue
		}
		filteredMetas = append(filteredMetas, m)
	}
	metas = filteredMetas

	send(progress, "extracting",
		fmt.Sprintf("Analysis complete: %d sessions processed", len(facets)), 0.70)

	// Phase 3: Aggregate data.
	send(progress, "generating", "Aggregating statistics...", 0.72)
	agg := Aggregate(metas, facets)

	// Phase 4: Generate insights via LLM.
	send(progress, "generating",
		fmt.Sprintf("Generating %d insight sections in parallel...", len(insightSections)), 0.75)

	sections, glance, err := GenerateInsights(ctx, cfg.Client, cfg.Model, agg, facets, progress)
	if err != nil {
		// LLM generation failed — still produce a stats-only report.
		send(progress, "generating",
			fmt.Sprintf("Insight generation failed: %v — producing stats-only report", err), 0.90)
		sections = nil
		glance = AtAGlance{}
	}

	send(progress, "synthesizing", "Generating HTML report...", 0.95)

	report := &Report{
		AtAGlance:   glance,
		Sections:    sections,
		Stats:       agg,
		GeneratedAt: time.Now(),
	}

	// Generate HTML report.
	if htmlPath, err := GenerateHTML(outputDir, report); err == nil {
		report.HTMLPath = htmlPath
	}

	progress <- ProgressEvent{
		Phase:  "done",
		Detail: "Report ready",
		Pct:    1.0,
		Report: report,
	}
}

// FormatReport renders the report as markdown for display in the TUI.
func FormatReport(r *Report) string {
	var b strings.Builder

	b.WriteString("# Session Insights\n\n")
	b.WriteString(fmt.Sprintf("_Generated %s · %d sessions · %d messages · %.1f hours_\n\n",
		r.GeneratedAt.Format("2006-01-02 15:04"),
		r.Stats.TotalSessions,
		r.Stats.TotalMessages,
		r.Stats.TotalDurationH,
	))

	// HTML report link.
	if r.HTMLPath != "" {
		b.WriteString(fmt.Sprintf("**Full report:** `%s`\n\n", r.HTMLPath))
	}

	// At a Glance.
	b.WriteString("## At a Glance\n\n")
	if r.AtAGlance.WhatsWorking != "" {
		b.WriteString("**What's working:** " + r.AtAGlance.WhatsWorking + "\n\n")
	}
	if r.AtAGlance.WhatsHindering != "" {
		b.WriteString("**What's hindering you:** " + r.AtAGlance.WhatsHindering + "\n\n")
	}
	if r.AtAGlance.QuickWins != "" {
		b.WriteString("**Quick wins to try:** " + r.AtAGlance.QuickWins + "\n\n")
	}
	if r.AtAGlance.AmbitiousWorkflows != "" {
		b.WriteString("**Ambitious workflows:** " + r.AtAGlance.AmbitiousWorkflows + "\n\n")
	}

	// Sections.
	for _, s := range r.Sections {
		b.WriteString("## " + s.Title + "\n\n")
		b.WriteString(s.Content + "\n\n")
	}

	return b.String()
}

func send(ch chan<- ProgressEvent, phase, detail string, pct float64) {
	ch <- ProgressEvent{Phase: phase, Detail: detail, Pct: pct}
}

func sendErr(ch chan<- ProgressEvent, err error) {
	ch <- ProgressEvent{Phase: "error", Detail: err.Error(), Err: err}
}

// isWarmupOnly returns true if the facet has exactly one positive goal category
// and that category is "warmup_minimal", indicating a trivial/test session.
func isWarmupOnly(f Facet) bool {
	if len(f.GoalCategories) == 0 {
		return false
	}
	var cats []string
	for cat, cnt := range f.GoalCategories {
		if cnt > 0 {
			cats = append(cats, cat)
		}
	}
	return len(cats) == 1 && cats[0] == "warmup_minimal"
}
