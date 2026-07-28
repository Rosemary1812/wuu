package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	wuuexec "github.com/blueberrycongee/wuu/internal/exec"
	"github.com/blueberrycongee/wuu/internal/securefs"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

var debugSandboxNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type debugSandboxCLIConfig struct {
	name      *string
	temporary *bool
}

type debugSandboxRecord struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

type debugSandboxListResult struct {
	Sandboxes []debugSandboxRecord `json:"sandboxes"`
}

type debugSandboxDeleteResult struct {
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	Deleted bool   `json:"deleted"`
}

func addDebugSandboxFlags(fs *flag.FlagSet) debugSandboxCLIConfig {
	return debugSandboxCLIConfig{
		name:      fs.String("sandbox-name", "", "named persistent debug sandbox"),
		temporary: fs.Bool("temp-sandbox", false, "use a temporary debug sandbox"),
	}
}

// normalizeDebugSandboxArgs preserves the original bare --sandbox behavior.
// Commands without an existing positional meaning may also allow --sandbox NAME.
func normalizeDebugSandboxArgs(args []string, allowBareName bool) ([]string, error) {
	normalized := make([]string, 0, len(args)+1)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--sandbox=") {
			name := strings.TrimSpace(strings.TrimPrefix(arg, "--sandbox="))
			if name == "" {
				return nil, errors.New("sandbox name cannot be empty")
			}
			if strings.EqualFold(name, "true") || strings.EqualFold(name, "false") {
				normalized = append(normalized, "--temp-sandbox="+strings.ToLower(name))
				continue
			}
			normalized = append(normalized, "--sandbox-name="+name)
			continue
		}
		if arg != "--sandbox" {
			normalized = append(normalized, arg)
			continue
		}
		if allowBareName && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			normalized = append(normalized, "--sandbox-name", args[i+1])
			i++
			continue
		}
		normalized = append(normalized, "--temp-sandbox")
	}
	return normalized, nil
}

func applyDebugSandboxOptions(opts *debugAppServerOptions, cfg debugSandboxCLIConfig) error {
	name := strings.TrimSpace(valueOfStringFlag(cfg.name))
	temporary := valueOfBoolFlag(cfg.temporary)
	if name != "" && temporary {
		return errors.New("choose a named sandbox or a temporary sandbox, not both")
	}
	if name != "" {
		if err := validateDebugSandboxName(name); err != nil {
			return err
		}
		opts.sandboxName = name
	}
	opts.sandbox = temporary
	return nil
}

func validateDebugSandboxName(name string) error {
	if !debugSandboxNamePattern.MatchString(strings.TrimSpace(name)) {
		return errors.New("sandbox name must be 1-64 characters using letters, numbers, dot, underscore, or hyphen")
	}
	return nil
}

func debugSandboxBaseDir(wuuHome string) string {
	return filepath.Join(wuuHome, "debug", "sandboxes")
}

func debugNamedSandboxDir(wuuHome, name string) (string, error) {
	if err := validateDebugSandboxName(name); err != nil {
		return "", err
	}
	return filepath.Join(debugSandboxBaseDir(wuuHome), name), nil
}

func activateDebugSandbox(wuuHome, name string, keepTemporary bool) (string, func(), error) {
	debugSandboxMu.Lock()
	removeOnCleanup := false
	stateHome := ""
	temporaryRoot := ""
	if strings.TrimSpace(name) != "" {
		var err error
		stateHome, err = debugNamedSandboxDir(wuuHome, name)
		if err != nil {
			debugSandboxMu.Unlock()
			return "", nil, err
		}
		baseDir := debugSandboxBaseDir(wuuHome)
		if err := securefs.Mkdir(baseDir); err != nil {
			debugSandboxMu.Unlock()
			return "", nil, fmt.Errorf("create named debug sandbox: %w", err)
		}
		if err := requireDebugSandboxDirectory(baseDir); err != nil {
			debugSandboxMu.Unlock()
			return "", nil, err
		}
		if err := securefs.Mkdir(stateHome); err != nil {
			debugSandboxMu.Unlock()
			return "", nil, fmt.Errorf("create named debug sandbox: %w", err)
		}
		if err := requireDebugSandboxDirectory(stateHome); err != nil {
			debugSandboxMu.Unlock()
			return "", nil, err
		}
	} else {
		root, err := os.MkdirTemp("", "wuu-channel-e2e-")
		if err != nil {
			debugSandboxMu.Unlock()
			return "", nil, fmt.Errorf("create channel e2e sandbox: %w", err)
		}
		stateHome = filepath.Join(root, "wuu-home")
		temporaryRoot = root
		removeOnCleanup = !keepTemporary
	}
	previous, existed := os.LookupEnv("WUU_HOME")
	if err := os.Setenv("WUU_HOME", stateHome); err != nil {
		if temporaryRoot != "" {
			_ = os.RemoveAll(temporaryRoot)
		}
		debugSandboxMu.Unlock()
		return "", nil, fmt.Errorf("activate debug sandbox: %w", err)
	}
	cleanup := func() {
		if existed {
			_ = os.Setenv("WUU_HOME", previous)
		} else {
			_ = os.Unsetenv("WUU_HOME")
		}
		if removeOnCleanup {
			_ = os.RemoveAll(temporaryRoot)
		}
		debugSandboxMu.Unlock()
	}
	return stateHome, cleanup, nil
}

func requireDebugSandboxDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect debug sandbox directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("debug sandbox path %q must be a real directory", path)
	}
	return nil
}

func runDebugSandbox(args []string) error {
	if len(args) == 0 {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("debug sandbox subcommand is required"))
	}
	switch args[0] {
	case "list":
		return runDebugSandboxList(args[1:])
	case "delete":
		return runDebugSandboxDelete(args[1:])
	default:
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, fmt.Errorf("unknown debug sandbox subcommand %q", args[0]))
	}
}

func runDebugSandboxList(args []string) error {
	fs := flag.NewFlagSet("debug sandbox list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	if fs.NArg() != 0 {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("debug sandbox list does not accept positional arguments"))
	}
	wuuHome, err := statepath.Home("")
	if err != nil {
		return err
	}
	baseDir := debugSandboxBaseDir(wuuHome)
	if err := requireDebugSandboxDirectory(baseDir); errors.Is(err, os.ErrNotExist) {
		return printJSON(debugSandboxListResult{Sandboxes: []debugSandboxRecord{}})
	} else if err != nil {
		return err
	}
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return err
	}
	result := debugSandboxListResult{Sandboxes: []debugSandboxRecord{}}
	for _, entry := range entries {
		if !entry.IsDir() || validateDebugSandboxName(entry.Name()) != nil {
			continue
		}
		result.Sandboxes = append(result.Sandboxes, debugSandboxRecord{
			Name: entry.Name(), Dir: filepath.Join(baseDir, entry.Name()),
		})
	}
	sort.Slice(result.Sandboxes, func(i, j int) bool { return result.Sandboxes[i].Name < result.Sandboxes[j].Name })
	return printJSON(result)
}

func runDebugSandboxDelete(args []string) error {
	fs := flag.NewFlagSet("debug sandbox delete", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	if fs.NArg() != 1 {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("debug sandbox delete requires exactly one sandbox name"))
	}
	name := strings.TrimSpace(fs.Arg(0))
	if err := validateDebugSandboxName(name); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	wuuHome, err := statepath.Home("")
	if err != nil {
		return err
	}
	baseDir := debugSandboxBaseDir(wuuHome)
	if err := requireDebugSandboxDirectory(baseDir); errors.Is(err, os.ErrNotExist) {
		dir, _ := debugNamedSandboxDir(wuuHome, name)
		return printJSON(debugSandboxDeleteResult{Name: name, Dir: dir, Deleted: false})
	} else if err != nil {
		return err
	}
	dir, err := debugNamedSandboxDir(wuuHome, name)
	if err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	_, statErr := os.Lstat(dir)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	deleted := statErr == nil
	if deleted {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("delete debug sandbox %q: %w", name, err)
		}
	}
	return printJSON(debugSandboxDeleteResult{Name: name, Dir: dir, Deleted: deleted})
}
