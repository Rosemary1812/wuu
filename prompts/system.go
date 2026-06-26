package prompts

import (
	_ "embed"
	"strings"
)

//go:embed system.md
var system string

func System() string {
	return strings.TrimSpace(system)
}
