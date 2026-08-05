package pluginhost

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ProtocolName    = "wuu-plugin-v1"
	ProtocolVersion = 1

	// maxResponseLineSize bounds the JSON-lines wire format so a single
	// plugin response cannot grow unbounded in memory. The limit applies to
	// one line including its trailing newline.
	maxResponseLineSize = 4 << 20 // 4 MiB
)

type ProcessConfig struct {
	ID          string
	Command     string
	Args        []string
	Env         map[string]string
	PluginRoot  string
	ProjectRoot string
	WuuHome     string
	Timeout     time.Duration
}

type rpcRequest struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Message string `json:"message"`
}

type ProcessClient struct {
	config  ProcessConfig
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	seq     atomic.Uint64
	callMu  sync.Mutex
	mu      sync.RWMutex
	status  Status
	stderr  lockedBuffer
}

func Start(ctx context.Context, config ProcessConfig) (*ProcessClient, error) {
	config.ID = strings.TrimSpace(config.ID)
	config.Command = strings.TrimSpace(config.Command)
	if config.ID == "" || config.Command == "" {
		return nil, errors.New("plugin process requires id and command")
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}

	cmd := exec.Command(config.Command, config.Args...)
	cmd.Dir = config.PluginRoot
	cmd.Env = buildEnv(config.Env)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin %q stdout: %w", config.ID, err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin %q stdin: %w", config.ID, err)
	}
	client := &ProcessClient{
		config: config,
		cmd:    cmd,
		stdin:  stdin,
		status: Status{ID: config.ID, State: StateStarting},
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 4096), maxResponseLineSize)
	client.scanner = scanner
	cmd.Stderr = &client.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start plugin %q: %w", config.ID, err)
	}

	initCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()
	var initialized InitializeResult
	if err := client.call(initCtx, "initialize", InitializeParams{
		ProtocolVersion: ProtocolVersion,
		PluginID:        config.ID,
		PluginRoot:      config.PluginRoot,
		ProjectRoot:     config.ProjectRoot,
		WuuHome:         config.WuuHome,
	}, &initialized); err != nil {
		_ = client.stopProcess()
		client.setFailure(err)
		return nil, fmt.Errorf("initialize plugin %q: %w", config.ID, err)
	}
	seen := make(map[Hook]struct{}, len(initialized.Hooks))
	for _, hook := range initialized.Hooks {
		if !IsValidHook(hook) {
			_ = client.stopProcess()
			return nil, fmt.Errorf("initialize plugin %q: unknown hook %q", config.ID, hook)
		}
		if _, ok := seen[hook]; ok {
			_ = client.stopProcess()
			return nil, fmt.Errorf("initialize plugin %q: duplicate hook %q", config.ID, hook)
		}
		seen[hook] = struct{}{}
	}
	client.mu.Lock()
	client.status.State = StateActive
	client.status.Hooks = append([]Hook(nil), initialized.Hooks...)
	client.status.StartedAt = time.Now().UTC()
	client.mu.Unlock()
	return client, nil
}

func (c *ProcessClient) ID() string { return c.config.ID }

func (c *ProcessClient) Hooks() []Hook {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]Hook(nil), c.status.Hooks...)
}

func (c *ProcessClient) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	status := c.status
	status.Hooks = append([]Hook(nil), status.Hooks...)
	return status
}

func (c *ProcessClient) Invoke(ctx context.Context, params InvokeParams) (InvokeResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	var result InvokeResult
	if err := c.call(callCtx, "hook.invoke", params, &result); err != nil {
		c.setFailure(err)
		return InvokeResult{}, err
	}
	return result, nil
}

func (c *ProcessClient) Close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = c.call(shutdownCtx, "shutdown", nil, nil)
	err := c.stopProcess()
	c.mu.Lock()
	c.status.State = StateStopped
	c.mu.Unlock()
	return err
}

func (c *ProcessClient) call(ctx context.Context, method string, params, result any) error {
	c.callMu.Lock()
	defer c.callMu.Unlock()
	if c.cmd == nil || c.cmd.Process == nil {
		return errors.New("plugin process is not running")
	}
	id := fmt.Sprintf("%d", c.seq.Add(1))
	payload, err := json.Marshal(rpcRequest{ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if _, err := c.stdin.Write(payload); err != nil {
		return fmt.Errorf("write request: %w", err)
	}

	type readResult struct {
		line []byte
		err  error
	}
	readCh := make(chan readResult, 1)
	go func() {
		if c.scanner.Scan() {
			line := c.scanner.Bytes()
			// Copy the token: the scanner reuses its buffer across calls.
			buf := make([]byte, len(line))
			copy(buf, line)
			readCh <- readResult{line: buf, err: nil}
			return
		}
		readErr := c.scanner.Err()
		if readErr == nil {
			readErr = io.EOF
		}
		if errors.Is(readErr, bufio.ErrTooLong) {
			readErr = fmt.Errorf("plugin response line exceeds %d bytes", maxResponseLineSize)
		}
		readCh <- readResult{err: readErr}
	}()
	select {
	case <-ctx.Done():
		_ = c.stopProcess()
		return ctx.Err()
	case read := <-readCh:
		if read.err != nil {
			return fmt.Errorf("read response: %w%s", read.err, c.stderrSuffix())
		}
		var response rpcResponse
		if err := json.Unmarshal(bytes.TrimSpace(read.line), &response); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		if response.ID != id {
			return fmt.Errorf("response id %q does not match request %q", response.ID, id)
		}
		if response.Error != nil {
			return errors.New(strings.TrimSpace(response.Error.Message))
		}
		if result == nil {
			return nil
		}
		if len(response.Result) == 0 {
			return errors.New("response is missing result")
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("decode result: %w", err)
		}
		return nil
	}
}

func (c *ProcessClient) stopProcess() error {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return nil
	}
	_ = c.stdin.Close()
	_ = c.cmd.Process.Kill()
	err := c.cmd.Wait()
	if err != nil && !strings.Contains(err.Error(), "signal: killed") {
		return err
	}
	return nil
}

func (c *ProcessClient) setFailure(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.State = StateFailed
	c.status.Error = err.Error()
}

func (c *ProcessClient) stderrSuffix() string {
	value := strings.TrimSpace(c.stderr.String())
	if value == "" {
		return ""
	}
	return ": " + value
}

// buildEnv returns a minimal, deterministic environment for a plugin process.
//
// Only a documented cross-platform baseline is inherited from the parent
// process (PATH, HOME, temp directories, locale variables, and Windows launch
// essentials). The full parent os.Environ is intentionally not inherited, so
// ambient API keys, tokens, and other secrets cannot leak into plugins. Values
// supplied in ProcessConfig.Env are overlaid on the baseline and always take
// precedence. The resulting slice is sorted by key for deterministic ordering.
func buildEnv(explicit map[string]string) []string {
	values := make(map[string]string, len(baselineEnvKeys)+len(explicit))
	for _, key := range baselineEnvKeys {
		if v := os.Getenv(key); v != "" {
			values[key] = v
		}
	}
	for key, value := range explicit {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
}

// baselineEnvKeys lists the small set of parent variables that plugins need to
// start command-line binaries and resolve basic paths. It intentionally omits
// credential- and tool-specific variables.
var baselineEnvKeys = func() []string {
	keys := []string{
		"PATH",
		"HOME",
		"USER",
		"LOGNAME",
		"SHELL",
		"TMPDIR",
		"TMP",
		"TEMP",
		"LANG",
		"LC_ALL",
		"LC_CTYPE",
		"LC_MESSAGES",
		"LC_NUMERIC",
		"LC_TIME",
		"LC_COLLATE",
		"LC_MONETARY",
		"LC_PAPER",
		"LC_NAME",
		"LC_ADDRESS",
		"LC_TELEPHONE",
		"LC_MEASUREMENT",
		"LC_IDENTIFICATION",
	}
	if runtime.GOOS == "windows" {
		keys = append(keys,
			"SYSTEMROOT",
			"SYSTEMDRIVE",
			"USERPROFILE",
			"APPDATA",
			"LOCALAPPDATA",
			"ProgramFiles",
			"ProgramFiles(x86)",
			"ProgramData",
			"CommonProgramFiles",
			"CommonProgramFiles(x86)",
			"PROCESSOR_ARCHITECTURE",
			"PROCESSOR_IDENTIFIER",
			"COMPUTERNAME",
			"USERNAME",
		)
	}
	return keys
}()

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}
