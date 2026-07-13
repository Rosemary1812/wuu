package providers

import (
	"os"
	"testing"
)

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
