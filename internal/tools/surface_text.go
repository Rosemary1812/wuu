package tools

import (
	"regexp"
	"strings"

	"github.com/blueberrycongee/wuu/internal/capability"
)

var terminalPathMarkerRE = regexp.MustCompile(`(?i)(^|[^a-z0-9])(?:bash|git|run_shell|run_test|start_process|list_processes|read_process_output|write_stdin|stop_process|command\.bash|terminal|shell)([^a-z0-9]|$)`)

func mentionsTerminalOnlyPath(parts ...string) bool {
	return terminalPathMarkerRE.MatchString(strings.Join(parts, "\n"))
}

func activeSurfaceLacksTerminalExecution(env *Env) bool {
	return env != nil &&
		env.ActiveSurface.ProfileName != "" &&
		!surfaceHasVisibleCapability(env.ActiveSurface, capability.CapabilityCommandBash)
}
