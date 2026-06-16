package goal

import (
	"fmt"
	"strings"
)

type FailureSyncResult struct {
	Added   int   `json:"added"`
	Skipped int   `json:"skipped"`
	State   State `json:"state"`
}

// SyncSnapshotFailures writes attention items from workflow and harness
// projections into the goal failure ledger. Callers must pass the intended
// goal store explicitly; this function does not discover or guess an active
// goal from the workspace.
func SyncSnapshotFailures(store *Store, snapshot SystemSnapshot) (FailureSyncResult, error) {
	if store == nil {
		return FailureSyncResult{}, fmt.Errorf("goal failure sync requires a store")
	}
	var result FailureSyncResult
	state, err := store.LoadState()
	if err != nil {
		return result, err
	}
	result.State = state
	for _, item := range snapshot.Attention {
		failure, ok := failureFromAttention(item)
		if !ok {
			continue
		}
		before := len(result.State.Failures)
		next, err := store.AddFailure(failure)
		if err != nil {
			return result, err
		}
		if len(next.Failures) == before {
			result.Skipped++
		} else {
			result.Added++
		}
		result.State = next
	}
	return result, nil
}

func failureFromAttention(item AttentionItem) (Failure, bool) {
	source := strings.TrimSpace(item.Source)
	status := strings.TrimSpace(item.Status)
	if source == "" && status == "" {
		return Failure{}, false
	}
	if !attentionRequiresFailure(status) {
		return Failure{}, false
	}
	sourceID := attentionSourceID(item)
	message := strings.TrimSpace(item.Message)
	if message == "" {
		message = fmt.Sprintf("%s %s requires attention", source, status)
	}
	return Failure{
		Step:     StepExecution,
		Kind:     failureKind(source, status),
		Source:   source,
		SourceID: sourceID,
		Message:  message,
		Artifact: strings.TrimSpace(item.Path),
	}, true
}

func attentionRequiresFailure(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error", "stuck", "partial", "missing_report", "changed_file_overlap", "awaiting_report", "paused", "needs_human":
		return true
	default:
		return false
	}
}

func attentionSourceID(item AttentionItem) string {
	parts := []string{
		strings.TrimSpace(item.ID),
		strings.TrimSpace(item.Status),
		strings.TrimSpace(item.Path),
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return strings.TrimSpace(item.Message)
	}
	return strings.Join(out, ":")
}

func failureKind(source, status string) string {
	source = strings.TrimSpace(source)
	status = strings.TrimSpace(status)
	if source == "" {
		return sanitizeFailureKind(status)
	}
	if status == "" {
		return sanitizeFailureKind(source)
	}
	return sanitizeFailureKind(source + "_" + status)
}

func sanitizeFailureKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "external_failure"
	}
	return out
}
