package prompts

import (
	"strings"
	"testing"
)

func TestSystemMainTeachesInceptionDMailTriggers(t *testing.T) {
	prompt := SystemMain()
	for _, want := range []string{
		"Use it proactively during the work",
		"large file",
		"web/search result",
		"dead end",
		"coding or debugging detour",
		"Do not wait until only the final answer remains",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("SystemMain missing %q:\n%s", want, prompt)
		}
	}
}
