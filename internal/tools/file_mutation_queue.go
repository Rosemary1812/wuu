package tools

import (
	"path/filepath"
	"sync"
)

type fileMutationQueueEntry struct {
	mu   sync.Mutex
	refs int
}

var fileMutationQueues = struct {
	sync.Mutex
	entries map[string]*fileMutationQueueEntry
}{entries: make(map[string]*fileMutationQueueEntry)}

// withFileMutationQueue serializes mutations to the same physical file while
// allowing unrelated files to proceed independently. The queue is process-wide
// so cloned toolkits targeting one workspace share the same mutation boundary.
func withFileMutationQueue[T any](path string, mutate func() (T, error)) (T, error) {
	key := fileMutationQueueKey(path)

	fileMutationQueues.Lock()
	entry := fileMutationQueues.entries[key]
	if entry == nil {
		entry = &fileMutationQueueEntry{}
		fileMutationQueues.entries[key] = entry
	}
	entry.refs++
	fileMutationQueues.Unlock()

	entry.mu.Lock()
	defer func() {
		entry.mu.Unlock()
		fileMutationQueues.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(fileMutationQueues.entries, key)
		}
		fileMutationQueues.Unlock()
	}()

	return mutate()
}

func fileMutationQueueKey(path string) string {
	cleaned := filepath.Clean(path)
	if canonical, err := filepath.EvalSymlinks(cleaned); err == nil {
		return canonical
	}
	return cleaned
}
