package tools

import "testing"

func TestConfigureWorkerSurfaceForProviderModel(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	kit.ConfigureWorkerSurfaceForProviderModel("openai", "gpt-5-codex", false)
	defaultNames := stringSet(kit.SurfaceToolNames())
	if defaultNames["spawn_agent"] || defaultNames["send_message"] || defaultNames["close_agent"] {
		t.Fatalf("default worker unexpectedly orchestrates: %v", defaultNames)
	}
	if !defaultNames["agent_report"] {
		t.Fatal("default worker missing agent_report")
	}

	kit.ConfigureWorkerSurfaceForProviderModel("openai", "gpt-5-codex", true)
	ultraNames := stringSet(kit.SurfaceToolNames())
	for _, name := range []string{"spawn_agent", "send_message", "close_agent", "agent_report"} {
		if !ultraNames[name] {
			t.Errorf("Ultra worker missing %s", name)
		}
	}
	if ultraNames["helpme"] {
		t.Fatal("Ultra worker must not expose helpme")
	}
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}
