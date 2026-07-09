package providers

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/securefs"
)

var (
	debugLog  *os.File
	debugOnce sync.Once
)

// InitDebugLog opens a debug log file in the given user-level log directory.
func InitDebugLog(logDir string) {
	debugOnce.Do(func() {
		// Directory at 0o700, file at 0o600 — the debug log captures raw
		// request / response metadata and provider latency, both of which
		// are credential-adjacent even when the body itself is opt-in.
		if err := securefs.Mkdir(logDir); err != nil {
			return
		}
		path := filepath.Join(logDir, "debug.log")
		// Rotate if the log exceeds 2 MB to prevent unbounded growth.
		if info, err := os.Stat(path); err == nil && info.Size() > 2*1024*1024 {
			prev := path + ".1"
			os.Remove(prev)
			os.Rename(path, prev)
		}
		f, err := securefs.OpenAppend(path)
		if err != nil {
			return
		}
		debugLog = f
		DebugLogf("=== wuu debug log started at %s ===", time.Now().Format(time.RFC3339))
	})
}

// DebugLogf writes a formatted line to the debug log.
func DebugLogf(format string, args ...any) {
	if debugLog == nil {
		return
	}
	fmt.Fprintf(debugLog, "[%s] %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
	debugLog.Sync()
}
