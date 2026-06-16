package goal

import (
	"fmt"
	"strings"
)

func resolveExternalGoalStore(configured *Store, goalDir, source string) (*Store, bool, error) {
	if configured != nil {
		return configured, true, nil
	}
	goalDir = strings.TrimSpace(goalDir)
	if goalDir == "" {
		return nil, false, nil
	}
	store := NewStore(goalDir)
	if _, err := store.LoadState(); err != nil {
		source = strings.TrimSpace(source)
		if source == "" {
			source = "external"
		}
		return nil, false, fmt.Errorf("load goal state for %s sync: %w", source, err)
	}
	return store, true, nil
}
