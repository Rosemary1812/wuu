package appserver

import (
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

const (
	defaultThreadSearchLimit = 40
	maxThreadSearchLimit     = 100
	threadSearchSnippetRunes = 180
)

type threadSearchSource struct {
	entry   threadListEntry
	history []providers.ChatMessage
}

type threadSearchCandidate struct {
	text string
}

func (s *Server) handleThreadSearch(req Request) error {
	var params ThreadSearchParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	query := normalizeThreadSearchText(params.Query)
	limit := params.Limit
	if limit <= 0 {
		limit = defaultThreadSearchLimit
	}
	if limit > maxThreadSearchLimit {
		limit = maxThreadSearchLimit
	}

	sources, err := s.threadSearchSources()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	results := make([]threadSearchResultEntry, 0, min(len(sources), limit))
	for _, source := range sources {
		candidates := threadSearchCandidates(source.entry.thread, source.history)
		snippet := ""
		if query != "" {
			snippet = threadSearchMatchSnippet(candidates, query)
			if snippet == "" {
				continue
			}
		} else {
			snippet = threadSearchDefaultSnippet(candidates)
		}
		thread, err := s.threadWithChildAgents(source.entry.thread)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		results = append(results, threadSearchResultEntry{
			entry:   source.entry,
			result:  ThreadSearchResultItem{Thread: thread, Snippet: snippet},
			matches: query != "",
		})
		if len(results) >= limit {
			break
		}
	}
	sortThreadSearchResults(results)
	out := make([]ThreadSearchResultItem, 0, len(results))
	for _, result := range results {
		out = append(out, result.result)
	}
	return s.writeResponse(req.ID, ThreadSearchResult{Results: out}, nil)
}

func (s *Server) threadSearchSources() ([]threadSearchSource, error) {
	sessions, err := session.ListForCWD(s.rt.SessionDir, s.rt.RootDir, 0)
	if err != nil {
		return nil, err
	}
	sourcesByID := make(map[string]threadSearchSource, len(sessions))
	for _, sess := range sessions {
		if sess.ArchivedAt != nil {
			continue
		}
		entry := threadEntryFromSession(sess, s.rt.ProviderName, s.rt.Model)
		history, err := loadChatMessages(s.rt.SessionDir, sess.ID)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			history = nil
		}
		sourcesByID[sess.ID] = threadSearchSource{entry: entry, history: history}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, th := range s.threads {
		th.mu.Lock()
		thread := th.snapshotLocked()
		history := cloneHistory(th.History)
		entry := threadListEntry{thread: thread, pinnedAt: th.PinnedAt}
		th.mu.Unlock()
		if thread.Ephemeral {
			continue
		}
		if thread.ReadOnly {
			continue
		}
		if thread.Archived {
			delete(sourcesByID, thread.ID)
			continue
		}
		if thread.CWD == s.rt.RootDir || thread.DMParticipantID != "" {
			sourcesByID[thread.ID] = threadSearchSource{
				entry:   entry,
				history: history,
			}
		}
	}

	sources := make([]threadSearchSource, 0, len(sourcesByID))
	for _, source := range sourcesByID {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool {
		return threadListEntryLess(sources[i].entry, sources[j].entry)
	})
	return sources, nil
}

type threadSearchResultEntry struct {
	entry   threadListEntry
	result  ThreadSearchResultItem
	matches bool
}

func sortThreadSearchResults(results []threadSearchResultEntry) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].matches != results[j].matches {
			return results[i].matches
		}
		return threadListEntryLess(results[i].entry, results[j].entry)
	})
}

func threadListEntryLess(left, right threadListEntry) bool {
	leftPinned := left.pinnedAt != nil
	rightPinned := right.pinnedAt != nil
	if leftPinned != rightPinned {
		return leftPinned
	}
	leftTime := threadListEntryTime(left)
	rightTime := threadListEntryTime(right)
	if !leftTime.Equal(rightTime) {
		return leftTime.After(rightTime)
	}
	return left.thread.ID > right.thread.ID
}

func threadListEntryTime(entry threadListEntry) time.Time {
	updatedAt := entry.thread.UpdatedAt
	if updatedAt.IsZero() {
		return entry.thread.CreatedAt
	}
	return updatedAt
}

func threadSearchCandidates(thread Thread, history []providers.ChatMessage) []threadSearchCandidate {
	candidates := []threadSearchCandidate{
		{text: thread.Preview},
		{text: thread.Model},
	}
	for _, msg := range history {
		if msg.Hidden {
			continue
		}
		candidates = append(candidates, threadSearchCandidatesFromMessage(msg)...)
	}
	return candidates
}

func threadSearchCandidatesFromMessage(msg providers.ChatMessage) []threadSearchCandidate {
	candidates := make([]threadSearchCandidate, 0, 4+len(msg.ToolCalls)*3)
	add := func(text string) {
		if compact := compactThreadSearchText(text); compact != "" {
			candidates = append(candidates, threadSearchCandidate{text: compact})
		}
	}
	add(msg.Content)
	add(msg.DisplayContent)
	add(msg.ReasoningContent)
	add(msg.Name)
	for _, call := range msg.ToolCalls {
		add(call.Name)
		add(call.Arguments)
		if call.Display != nil {
			add(call.Display.Text)
			add(call.Display.Kind)
		}
	}
	if len(msg.Images) == 1 {
		add("[Image #1]")
	} else if len(msg.Images) > 1 {
		add("images")
	}
	if len(msg.Files) == 1 {
		add(filePreview(msg.Files[0], 1))
	} else if len(msg.Files) > 1 {
		add("files")
	}
	return candidates
}

func threadSearchMatchSnippet(candidates []threadSearchCandidate, query string) string {
	for _, candidate := range candidates {
		if strings.Contains(normalizeThreadSearchText(candidate.text), query) {
			return threadSearchExcerpt(candidate.text, query)
		}
	}
	return ""
}

func threadSearchDefaultSnippet(candidates []threadSearchCandidate) string {
	for _, candidate := range candidates {
		if compact := compactThreadSearchText(candidate.text); compact != "" {
			return threadSearchExcerpt(compact, "")
		}
	}
	return ""
}

func threadSearchExcerpt(text, normalizedQuery string) string {
	compact := compactThreadSearchText(text)
	if compact == "" {
		return ""
	}
	runes := []rune(compact)
	if len(runes) <= threadSearchSnippetRunes {
		return compact
	}
	matchIndex := -1
	normalized := ""
	if normalizedQuery != "" {
		normalized = normalizeThreadSearchText(compact)
		matchIndex = strings.Index(normalized, normalizedQuery)
	}
	start := 0
	if matchIndex > 0 {
		start = max(0, len([]rune(normalized[:matchIndex]))-40)
	}
	end := min(len(runes), start+threadSearchSnippetRunes)
	prefix := ""
	if start > 0 {
		prefix = "..."
	}
	suffix := ""
	if end < len(runes) {
		suffix = "..."
	}
	return prefix + string(runes[start:end]) + suffix
}

func normalizeThreadSearchText(value string) string {
	return strings.ToLower(compactThreadSearchText(value))
}

func compactThreadSearchText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
