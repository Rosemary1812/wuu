package loop

import (
	"fmt"
	"strings"
)

func resolveExternalLoopStore(configured *Store, loopDir, source string) (*Store, bool, error) {
	if configured != nil {
		return configured, true, nil
	}
	loopDir = strings.TrimSpace(loopDir)
	if loopDir == "" {
		return nil, false, nil
	}
	store := NewStore(loopDir)
	if _, err := store.LoadState(); err != nil {
		source = strings.TrimSpace(source)
		if source == "" {
			source = "external"
		}
		return nil, false, fmt.Errorf("load loop state for %s sync: %w", source, err)
	}
	return store, true, nil
}
