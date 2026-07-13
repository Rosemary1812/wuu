package providers

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestDebugLogfConcurrentWritesAreSerialized exercises DebugLogf from many
// goroutines. With the per-line mutex, every line lands intact; without it (and
// under -race) the writes would interleave or race. It also implicitly confirms
// DebugLogf writes straight to the fd (no fsync, no buffering): the content is
// readable immediately from a separate handle.
func TestDebugLogfConcurrentWritesAreSerialized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "debug.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	debugMu.Lock()
	prev := debugLog
	debugLog = f
	debugMu.Unlock()
	t.Cleanup(func() {
		debugMu.Lock()
		debugLog = prev
		debugMu.Unlock()
		f.Close()
	})

	const writers, perWriter = 8, 64
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				DebugLogf("entry w=%d i=%d", w, i)
			}
		}(w)
	}
	wg.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "entry w="); got != writers*perWriter {
		t.Fatalf("wrote %d entries, want %d", got, writers*perWriter)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Count(line, "entry w=") > 1 {
			t.Fatalf("interleaved writers on one line: %q", line)
		}
	}
}

func TestWireEnabled_DefaultOff(t *testing.T) {
	// Default: env var unset → wire dumps are off.
	orig, hadOrig := os.LookupEnv(WireEnvVar)
	os.Unsetenv(WireEnvVar)
	t.Cleanup(func() {
		if hadOrig {
			os.Setenv(WireEnvVar, orig)
		}
	})
	if WireEnabled() {
		t.Fatalf("WireEnabled() = true with %s unset; want false", WireEnvVar)
	}
}

func TestWireEnabled_On(t *testing.T) {
	t.Setenv(WireEnvVar, "1")
	if !WireEnabled() {
		t.Fatalf("WireEnabled() = false with %s=1; want true", WireEnvVar)
	}
}

func TestWireEnabled_True(t *testing.T) {
	t.Setenv(WireEnvVar, "true")
	if !WireEnabled() {
		t.Fatalf("WireEnabled() = false with %s=true; want true", WireEnvVar)
	}
}

func TestWireEnabled_TrueCaseInsensitive(t *testing.T) {
	for _, v := range []string{"True", "TRUE", "tRuE"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(WireEnvVar, v)
			if !WireEnabled() {
				t.Fatalf("WireEnabled() = false with %s=%q; want true", WireEnvVar, v)
			}
		})
	}
}

func TestWireEnabled_Zero(t *testing.T) {
	t.Setenv(WireEnvVar, "0")
	if WireEnabled() {
		t.Fatalf("WireEnabled() = true with %s=0; want false", WireEnvVar)
	}
}

func TestWireEnabled_OtherText(t *testing.T) {
	// Anything that isn't "1" / "true" (case-insensitive) is treated as
	// off. We don't want a typo like "yes" or "on" to silently turn on
	// raw wire dumps.
	for _, v := range []string{"yes", "on", "2", "wire", "enabled"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(WireEnvVar, v)
			if WireEnabled() {
				t.Fatalf("WireEnabled() = true with %s=%q; want false", WireEnvVar, v)
			}
		})
	}
}

func TestWireEnabled_EmptyString(t *testing.T) {
	t.Setenv(WireEnvVar, "")
	if WireEnabled() {
		t.Fatalf("WireEnabled() = true with %s=\"\"; want false", WireEnvVar)
	}
}

func TestDebugLogfWire_NoLogFile(t *testing.T) {
	// Without InitDebugLog having been called, debugLog is nil and
	// DebugLogf is a no-op. The wire gate on top of that should also
	// be a no-op, never panicking on a nil log file.
	t.Setenv(WireEnvVar, "1")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DebugLogfWire panicked with no log file: %v", r)
		}
	}()
	DebugLogfWire("should not panic: %s", "test")
}

func TestDebugLogfWire_DisabledNoWrite(t *testing.T) {
	// With wire disabled (env unset / non-truthy), DebugLogfWire must
	// not produce any side effect — even if a log file exists. We
	// exercise this without spinning up InitDebugLog (sync.Once would
	// pin the file across tests), by checking that the public API
	// short-circuits cleanly.
	orig, hadOrig := os.LookupEnv(WireEnvVar)
	os.Unsetenv(WireEnvVar)
	t.Cleanup(func() {
		if hadOrig {
			os.Setenv(WireEnvVar, orig)
		}
	})
	if WireEnabled() {
		t.Fatalf("precondition: wire should be disabled")
	}
	// Just verify no panic; the gate prevents the inner DebugLogf
	// from running, so there's nothing observable to assert beyond
	// "this returns cleanly".
	DebugLogfWire("ignored because wire is off")
}
