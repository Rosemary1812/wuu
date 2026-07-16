package process

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/securefs"
	"github.com/blueberrycongee/wuu/internal/shellpath"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

type OwnerKind string
type Lifecycle string
type Status string
type EventType string
type EventCause string
type CompletionMode string

const (
	EventStarted   EventType = "started"
	EventUpdated   EventType = "updated"
	EventFailed    EventType = "failed"
	EventStopped   EventType = "stopped"
	EventCleanedUp EventType = "cleaned_up"

	EventCauseNaturalExit   EventCause = "natural_exit"
	EventCauseRequestedStop EventCause = "requested_stop"

	OwnerMainAgent OwnerKind = "main_agent"
	OwnerSubagent  OwnerKind = "subagent"

	LifecycleSession Lifecycle = "session"
	LifecycleManaged Lifecycle = "managed"

	CompletionModeResume   CompletionMode = "resume"
	CompletionModeDetached CompletionMode = "detached"

	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopping Status = "stopping"
	StatusStopped  Status = "stopped"
	StatusFailed   Status = "failed"
)

type Process struct {
	Action                string         `json:"action,omitempty"`
	ID                    string         `json:"id"`
	OwnerKind             OwnerKind      `json:"owner_kind"`
	OwnerID               string         `json:"owner_id"`
	Lifecycle             Lifecycle      `json:"lifecycle"`
	CompletionMode        CompletionMode `json:"completion_mode,omitempty"`
	Status                Status         `json:"status"`
	PID                   int            `json:"pid"`
	PGID                  int            `json:"pgid"`
	ProcessStartTime      string         `json:"process_start_time,omitempty"`
	TTY                   bool           `json:"tty,omitempty"`
	LogPath               string         `json:"log_path"`
	Command               string         `json:"command"`
	CWD                   string         `json:"cwd"`
	PreviewURLs           []string       `json:"preview_urls,omitempty"`
	PrimaryPreviewURL     string         `json:"primary_preview_url,omitempty"`
	StartedAt             time.Time      `json:"started_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
	StoppedAt             time.Time      `json:"stopped_at,omitempty"`
	ExitCode              int            `json:"exit_code,omitempty"`
	LastError             string         `json:"last_error,omitempty"`
	TerminalCause         EventCause     `json:"terminal_cause,omitempty"`
	CompletionDeliveredAt time.Time      `json:"completion_delivered_at,omitempty"`
	CompletionConsumedBy  string         `json:"completion_consumed_by,omitempty"`
}

type StartOptions struct {
	Command               string
	CWD                   string
	OwnerKind             OwnerKind
	OwnerID               string
	Lifecycle             Lifecycle
	CompletionMode        CompletionMode
	TTY                   bool
	AllowOutsideWorkspace bool
}

type Event struct {
	Type    EventType
	Cause   EventCause
	Process Process
}

type CleanupResult struct {
	Cleaned []Process
}

type OutputReadOptions struct {
	MaxBytes    int
	OffsetBytes *int64
	Wait        time.Duration
}

type OutputSnapshot struct {
	Process     Process
	Output      string
	Truncated   bool
	StartOffset int64
	EndOffset   int64
	TotalBytes  int64
	TimedOut    bool
	Duration    time.Duration
}

type Manager struct {
	rootDir     string
	registryDir string
	logDir      string
	mu          sync.Mutex
	subMu       sync.Mutex
	handles     map[string]*processHandle
	subscribers []chan<- Event
}

type processHandle struct {
	mu    sync.Mutex
	stdin io.WriteCloser
	done  chan struct{}
}

func NewManager(rootDir string, runtimeDirs ...string) (*Manager, error) {
	if strings.TrimSpace(rootDir) == "" {
		return nil, errors.New("root directory is required")
	}
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	runtimeDir := ""
	if len(runtimeDirs) > 0 {
		runtimeDir = strings.TrimSpace(runtimeDirs[0])
	}
	if runtimeDir == "" {
		wuuHome, err := statepath.Home("")
		if err != nil {
			return nil, err
		}
		workspaceStateDir, err := statepath.WorkspaceDir(wuuHome, abs)
		if err != nil {
			return nil, err
		}
		runtimeDir = statepath.RuntimeDir(workspaceStateDir)
	}
	runtimeDir, err = filepath.Abs(runtimeDir)
	if err != nil {
		return nil, err
	}
	m := &Manager{
		rootDir:     abs,
		registryDir: filepath.Join(runtimeDir, "processes"),
		logDir:      filepath.Join(runtimeDir, "logs"),
		handles:     make(map[string]*processHandle),
	}
	if err := os.MkdirAll(m.registryDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(m.logDir, 0o755); err != nil {
		return nil, err
	}
	if err := m.resumePersistedManagedProcesses(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) Start(ctx context.Context, opt StartOptions) (*Process, error) {
	if strings.TrimSpace(opt.Command) == "" {
		return nil, errors.New("command is required")
	}
	if opt.OwnerKind != OwnerMainAgent && opt.OwnerKind != OwnerSubagent {
		return nil, errors.New("owner_kind must be main_agent or subagent")
	}
	if opt.Lifecycle == "" {
		opt.Lifecycle = LifecycleSession
	}
	if opt.Lifecycle != LifecycleSession && opt.Lifecycle != LifecycleManaged {
		return nil, errors.New("lifecycle must be session or managed")
	}
	if opt.CompletionMode == "" {
		opt.CompletionMode = CompletionModeResume
	}
	if opt.CompletionMode != CompletionModeResume && opt.CompletionMode != CompletionModeDetached {
		return nil, errors.New("completion_mode must be resume or detached")
	}
	cwd, err := resolveStartCWD(m.rootDir, opt.CWD, opt.AllowOutsideWorkspace)
	if err != nil {
		return nil, err
	}
	if opt.TTY && !ptySupported() {
		// Degrade to pipe mode rather than failing the start: tty is a
		// fidelity feature, and platforms without pty support (Windows)
		// still run the process fine. The record keeps TTY=false so
		// readers see the mode that actually ran.
		opt.TTY = false
	}
	id := "proc-" + randomHex(4)
	p := &Process{ID: id, OwnerKind: opt.OwnerKind, OwnerID: opt.OwnerID, Lifecycle: opt.Lifecycle, CompletionMode: opt.CompletionMode, Status: StatusStarting, Command: opt.Command, CWD: cwd, TTY: opt.TTY, LogPath: filepath.Join(m.logDir, id+".log"), StartedAt: time.Now(), UpdatedAt: time.Now(), ExitCode: -1}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.save(p); err != nil {
		return nil, err
	}
	logf, err := os.OpenFile(p.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		p.Status = StatusFailed
		p.LastError = err.Error()
		_ = m.save(p)
		m.publish(Event{Type: EventFailed, Process: *p})
		return p, err
	}
	if err := ctx.Err(); err != nil {
		_ = logf.Close()
		p.Status = StatusFailed
		p.LastError = err.Error()
		p.UpdatedAt = time.Now()
		_ = m.save(p)
		m.publish(Event{Type: EventFailed, Process: *p})
		return p, err
	}
	cmd, err := managedCommand(opt.Command, cwd)
	if err != nil {
		_ = logf.Close()
		p.Status = StatusFailed
		p.LastError = err.Error()
		p.UpdatedAt = time.Now()
		_ = m.save(p)
		m.publish(Event{Type: EventFailed, Process: *p})
		return p, err
	}
	var stdin io.WriteCloser
	var ptyFile *os.File
	if opt.TTY {
		ptyFile, err = startPTYProcess(cmd)
		if err != nil {
			_ = logf.Close()
			p.Status = StatusFailed
			p.LastError = err.Error()
			p.UpdatedAt = time.Now()
			_ = m.save(p)
			m.publish(Event{Type: EventFailed, Process: *p})
			return p, fmt.Errorf("start pty process: %w", err)
		}
		stdin = ptyFile
	} else {
		stdin, err = cmd.StdinPipe()
		if err != nil {
			_ = logf.Close()
			p.Status = StatusFailed
			p.LastError = err.Error()
			p.UpdatedAt = time.Now()
			_ = m.save(p)
			m.publish(Event{Type: EventFailed, Process: *p})
			return p, fmt.Errorf("stdin pipe: %w", err)
		}
		cmd.Stdout = logf
		cmd.Stderr = logf
		configureProcessGroup(cmd)
		if err := cmd.Start(); err != nil {
			_ = stdin.Close()
			_ = logf.Close()
			p.Status = StatusFailed
			p.LastError = err.Error()
			p.UpdatedAt = time.Now()
			_ = m.save(p)
			m.publish(Event{Type: EventFailed, Process: *p})
			return p, fmt.Errorf("start process: %w", err)
		}
	}
	p.PID = cmd.Process.Pid
	p.PGID = lookupProcessGroup(p.PID)
	p.ProcessStartTime, _, _, err = readProcessIdentity(p.PID)
	if err != nil {
		if p.PGID > 1 {
			_ = killProcessGroup(p.PGID)
		}
		if stdin != nil {
			_ = stdin.Close()
		}
		_ = cmd.Wait()
		_ = logf.Close()
		p.Status = StatusFailed
		p.LastError = err.Error()
		p.UpdatedAt = time.Now()
		_ = m.save(p)
		m.publish(Event{Type: EventFailed, Process: *p})
		return p, fmt.Errorf("record process identity: %w", err)
	}
	p.Status = StatusRunning
	p.UpdatedAt = time.Now()
	_ = m.save(p)
	handle := &processHandle{stdin: stdin, done: make(chan struct{})}
	m.handles[id] = handle
	m.publish(Event{Type: EventStarted, Process: *p})
	if opt.TTY {
		go func() {
			defer close(handle.done)
			m.waitPTY(id, cmd, logf, ptyFile)
		}()
	} else {
		go func() {
			defer close(handle.done)
			m.wait(id, cmd, logf)
		}()
	}
	return p, nil
}

func resolveStartCWD(rootDir, cwd string, allowOutsideWorkspace bool) (string, error) {
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root %q: %w", rootDir, err)
	}
	if evaluatedRoot, err := filepath.EvalSymlinks(root); err == nil {
		root = evaluatedRoot
	}
	workDir := strings.TrimSpace(cwd)
	if workDir == "" {
		workDir = root
	} else if !filepath.IsAbs(workDir) {
		workDir = filepath.Join(root, workDir)
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return "", fmt.Errorf("resolve working directory %q: %w", workDir, err)
	}
	evaluated, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("working directory %q does not exist", abs)
		}
		return "", fmt.Errorf("inspect working directory %q: %w", abs, err)
	}
	info, err := os.Stat(evaluated)
	if err != nil {
		return "", fmt.Errorf("inspect working directory %q: %w", evaluated, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("working directory %q is not a directory", evaluated)
	}
	if allowOutsideWorkspace {
		return evaluated, nil
	}
	cmpRoot, cmpDir := root, evaluated
	if runtime.GOOS == "windows" {
		// Windows filesystems compare names case-insensitively.
		cmpRoot, cmpDir = strings.ToLower(cmpRoot), strings.ToLower(cmpDir)
	}
	rel, err := filepath.Rel(cmpRoot, cmpDir)
	if err != nil {
		return "", fmt.Errorf("resolve working directory %q against workspace root %q: %w", evaluated, root, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("working directory %q escapes workspace %q", evaluated, root)
	}
	return evaluated, nil
}

func managedCommand(command, cwd string) (*exec.Cmd, error) {
	shell, err := shellpath.LoginBash()
	if err != nil {
		return nil, err
	}
	command = shellpath.NormalizeBashCommand(command)
	cmd := exec.Command(shell.Path, shell.CommandArgs(command)...)
	cmd.Dir = cwd
	cmd.Env = shellpath.CommandEnv(os.Environ())
	return cmd, nil
}

func (m *Manager) wait(id string, cmd *exec.Cmd, logf *os.File) {
	err := cmd.Wait()
	_ = logf.Close()
	m.finishWait(id, cmd, err)
}

func (m *Manager) waitPTY(id string, cmd *exec.Cmd, logf *os.File, ptyFile *os.File) {
	copyDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(logf, ptyFile)
		close(copyDone)
	}()
	err := cmd.Wait()
	_ = ptyFile.Close()
	<-copyDone
	_ = logf.Close()
	m.finishWait(id, cmd, err)
}

func (m *Manager) finishWait(id string, cmd *exec.Cmd, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.handles, id)
	p, rerr := m.load(id)
	if rerr != nil {
		return
	}
	alreadyTerminal := p.Status == StatusStopped || p.Status == StatusFailed
	requestedStop := p.Status == StatusStopping || p.Status == StatusStopped
	if p.Status == StatusStopping || p.Status == StatusStopped {
		p.Status = StatusStopped
	} else if p.Status == StatusFailed {
		// Preserve an earlier failure recorded by the manager.
	} else if err != nil {
		p.Status = StatusFailed
		p.LastError = err.Error()
	} else {
		p.Status = StatusStopped
	}
	if cmd.ProcessState != nil {
		p.ExitCode = cmd.ProcessState.ExitCode()
	}
	p.StoppedAt = time.Now()
	p.UpdatedAt = time.Now()
	_ = m.save(p)
	if alreadyTerminal {
		return
	}
	eventType := EventStopped
	if p.Status == StatusFailed {
		eventType = EventFailed
	}
	cause := EventCauseNaturalExit
	if requestedStop {
		cause = EventCauseRequestedStop
	}
	p.TerminalCause = cause
	_ = m.save(p)
	m.publish(Event{Type: eventType, Cause: cause, Process: *p})
}

func (m *Manager) List() ([]Process, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	files, err := filepath.Glob(filepath.Join(m.registryDir, "*.json"))
	if err != nil {
		return nil, err
	}
	out := []Process{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("read process record %q: %w", filepath.Base(f), err)
		}
		var p Process
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("decode process record %q: %w", filepath.Base(f), err)
		}
		wantID := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
		if p.ID != wantID {
			return nil, fmt.Errorf("process record %q contains mismatched id %q", filepath.Base(f), p.ID)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, nil
}

func (m *Manager) UpdatePreview(id string, urls []string, primaryURL string) (*Process, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.load(id)
	if err != nil {
		return nil, err
	}
	normalized := NormalizePreviewURLs(urls)
	primary := NormalizePreviewURL(primaryURL)
	if primary != "" && !stringSliceContains(normalized, primary) {
		normalized = append([]string{primary}, normalized...)
	}
	if primary == "" && len(normalized) > 0 {
		primary = normalized[0]
	}
	p.PreviewURLs = normalized
	p.PrimaryPreviewURL = primary
	p.UpdatedAt = time.Now()
	if err := m.save(p); err != nil {
		return nil, err
	}
	m.publish(Event{Type: EventUpdated, Process: *p})
	return p, nil
}

func (m *Manager) ReadOutput(id string, maxBytes int) (string, bool, error) {
	snapshot, err := m.ReadOutputSnapshot(context.Background(), id, OutputReadOptions{MaxBytes: maxBytes})
	if err != nil {
		return "", false, err
	}
	return snapshot.Output, snapshot.Truncated, nil
}

func (m *Manager) ReadOutputSnapshot(ctx context.Context, id string, opt OutputReadOptions) (OutputSnapshot, error) {
	started := time.Now()
	if ctx == nil {
		ctx = context.Background()
	}
	if opt.MaxBytes <= 0 {
		opt.MaxBytes = 32 * 1024
	}
	if opt.Wait < 0 {
		opt.Wait = 0
	}
	p, err := m.load(id)
	if err != nil {
		return OutputSnapshot{}, err
	}
	timedOut := false
	if opt.Wait > 0 && opt.OffsetBytes != nil {
		deadline := time.Now().Add(opt.Wait)
		for {
			size, sizeErr := fileSize(p.LogPath)
			if sizeErr != nil {
				return OutputSnapshot{}, sizeErr
			}
			offset := *opt.OffsetBytes
			if offset < 0 {
				offset = 0
			}
			if size > offset || !isLiveStatus(p.Status) {
				break
			}
			if !time.Now().Before(deadline) {
				timedOut = true
				break
			}
			wait := 50 * time.Millisecond
			if remaining := time.Until(deadline); remaining < wait {
				wait = remaining
			}
			select {
			case <-ctx.Done():
				return OutputSnapshot{}, ctx.Err()
			case <-time.After(wait):
			}
			if latest, loadErr := m.load(id); loadErr == nil {
				p = latest
			}
		}
	}
	if latest, loadErr := m.load(id); loadErr == nil {
		p = latest
	}
	output, truncated, startOffset, endOffset, totalBytes, err := readLogWindow(p.LogPath, opt.MaxBytes, opt.OffsetBytes)
	if err != nil {
		return OutputSnapshot{}, err
	}
	return OutputSnapshot{
		Process:     *p,
		Output:      output,
		Truncated:   truncated,
		StartOffset: startOffset,
		EndOffset:   endOffset,
		TotalBytes:  totalBytes,
		TimedOut:    timedOut,
		Duration:    time.Since(started),
	}, nil
}

func readLogWindow(path string, maxBytes int, offsetBytes *int64) (string, bool, int64, int64, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, 0, 0, 0, err
	}
	defer f.Close()
	info, _ := f.Stat()
	size := info.Size()
	end := size
	start := int64(0)
	truncated := false
	if offsetBytes != nil {
		start = *offsetBytes
		if start < 0 {
			start = 0
		}
		if start > end {
			start = end
		}
		if end-start > int64(maxBytes) {
			start = end - int64(maxBytes)
			truncated = true
		}
	} else if size > int64(maxBytes) {
		start = size - int64(maxBytes)
		truncated = true
	}
	_, _ = f.Seek(start, 0)
	b, err := io.ReadAll(io.LimitReader(f, end-start))
	return string(b), truncated, start, end, size, err
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func isLiveStatus(status Status) bool {
	return status == StatusStarting || status == StatusRunning || status == StatusStopping
}

func (m *Manager) WriteStdin(id string, input string) (*Process, error) {
	m.mu.Lock()
	p, err := m.load(id)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if p.Status != StatusRunning && p.Status != StatusStarting {
		m.mu.Unlock()
		return p, fmt.Errorf("process %q is not running", id)
	}
	handle := m.handles[id]
	m.mu.Unlock()
	if handle == nil || handle.stdin == nil {
		return p, fmt.Errorf("stdin is not available for process %q", id)
	}
	handle.mu.Lock()
	defer handle.mu.Unlock()
	if _, err := io.WriteString(handle.stdin, input); err != nil {
		return p, fmt.Errorf("write stdin: %w", err)
	}
	return p, nil
}

func (m *Manager) Stop(id string) (*Process, error) {
	m.mu.Lock()
	p, err := m.load(id)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	handle := m.handles[id]
	if p.Status == StatusStopped || p.Status == StatusFailed {
		m.mu.Unlock()
		waitForProcessMonitor(handle)
		return p, nil
	}
	running, err := processMatchesRecord(p)
	if err != nil {
		if errors.Is(err, errNoRecordedIdentity) {
			// The record predates persisted process identities, so the live
			// process can never be verified against it. Never signal the pid;
			// retire the record so reconciliation does not retry it forever.
			return m.retireUnverifiableRecordLocked(id, p, err)
		}
		m.mu.Unlock()
		return p, err
	}
	if !running {
		delete(m.handles, id)
		p.Status = StatusStopped
		p.StoppedAt = time.Now()
		p.UpdatedAt = p.StoppedAt
		p.TerminalCause = EventCauseRequestedStop
		if err := m.save(p); err != nil {
			m.mu.Unlock()
			return p, err
		}
		m.mu.Unlock()
		m.publish(Event{Type: EventStopped, Cause: EventCauseRequestedStop, Process: *p})
		waitForProcessMonitor(handle)
		return p, nil
	}
	p.Status = StatusStopping
	p.UpdatedAt = time.Now()
	if err := m.save(p); err != nil {
		m.mu.Unlock()
		return p, fmt.Errorf("persist stopping process: %w", err)
	}
	m.mu.Unlock()
	if err := terminateProcessGroup(p.PGID); err != nil {
		return p, fmt.Errorf("terminate process group %d: %w", p.PGID, err)
	}
	cur, stopped, err := m.waitForStop(id, time.Now().Add(2*time.Second))
	if err != nil || stopped {
		if stopped {
			waitForProcessMonitor(handle)
		}
		return cur, err
	}

	running, err = processMatchesRecord(cur)
	if err != nil {
		return cur, err
	}
	if !running {
		cur, err := m.reconcileStopped(id)
		if err == nil {
			waitForProcessMonitor(handle)
		}
		return cur, err
	}
	if err := killProcessGroup(cur.PGID); err != nil {
		return cur, fmt.Errorf("kill process group %d: %w", cur.PGID, err)
	}
	cur, stopped, err = m.waitForStop(id, time.Now().Add(2*time.Second))
	if err != nil {
		return cur, err
	}
	if !stopped {
		return cur, fmt.Errorf("process group %d did not stop after SIGKILL", cur.PGID)
	}
	waitForProcessMonitor(handle)
	return cur, nil
}

func waitForProcessMonitor(handle *processHandle) {
	if handle == nil || handle.done == nil {
		return
	}
	<-handle.done
}

func (m *Manager) waitForStop(id string, deadline time.Time) (*Process, bool, error) {
	for {
		p, err := m.load(id)
		if err != nil {
			return nil, false, err
		}
		if p.Status == StatusStopped || p.Status == StatusFailed {
			return p, true, nil
		}
		if !processExists(p.PID) {
			stopped, err := m.reconcileStopped(id)
			return stopped, err == nil, err
		}
		if !time.Now().Before(deadline) {
			return p, false, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (m *Manager) reconcileStopped(id string) (*Process, error) {
	m.mu.Lock()
	p, err := m.load(id)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if p.Status == StatusStopped || p.Status == StatusFailed {
		m.mu.Unlock()
		return p, nil
	}
	delete(m.handles, id)
	p.Status = StatusStopped
	p.StoppedAt = time.Now()
	p.UpdatedAt = p.StoppedAt
	p.TerminalCause = EventCauseRequestedStop
	if err := m.save(p); err != nil {
		m.mu.Unlock()
		return p, err
	}
	m.mu.Unlock()
	m.publish(Event{Type: EventStopped, Cause: EventCauseRequestedStop, Process: *p})
	return p, nil
}

// retireUnverifiableRecordLocked ends management of a record whose process can
// never be verified. No signal is sent to the recorded pid; the record itself
// is marked failed and terminal so reconciliation does not retry it forever.
// The caller must hold m.mu; the lock is released before publishing.
func (m *Manager) retireUnverifiableRecordLocked(id string, p *Process, cause error) (*Process, error) {
	delete(m.handles, id)
	p.Status = StatusFailed
	p.LastError = fmt.Sprintf("record retired without signaling: %v", cause)
	p.StoppedAt = time.Now()
	p.UpdatedAt = p.StoppedAt
	p.TerminalCause = EventCauseRequestedStop
	if err := m.save(p); err != nil {
		m.mu.Unlock()
		return p, err
	}
	m.mu.Unlock()
	log.Printf("wuu: process %s has no recorded identity; retired its record without signaling pid %d", id, p.PID)
	m.publish(Event{Type: EventFailed, Cause: EventCauseRequestedStop, Process: *p})
	return p, nil
}

// errNoRecordedIdentity marks records that predate persisted process
// identities. A live process can never be verified against such a record, so
// the record must never be signaled; Stop retires it instead.
var errNoRecordedIdentity = errors.New("no recorded identity")

func processMatchesRecord(p *Process) (bool, error) {
	if p.PID <= 1 || !processExists(p.PID) {
		return false, nil
	}
	if p.PGID <= 1 {
		return false, fmt.Errorf("process %q has unsafe process group %d", p.ID, p.PGID)
	}
	storedIdentity := strings.TrimSpace(p.ProcessStartTime)
	if storedIdentity == "" {
		return false, fmt.Errorf("process %q has %w; refusing to signal pid %d", p.ID, errNoRecordedIdentity, p.PID)
	}
	currentIdentity, _, _, err := readProcessIdentity(p.PID)
	if err != nil {
		if !processExists(p.PID) {
			return false, nil
		}
		return false, fmt.Errorf("verify process %q identity: %w", p.ID, err)
	}
	groupRunning, err := verifyProcessGroup(p.PID, p.PGID)
	if err != nil {
		return false, fmt.Errorf("verify process %q group: %w", p.ID, err)
	}
	if !groupRunning {
		return false, nil
	}
	if storedIdentity == currentIdentity {
		return true, nil
	}
	if isLegacyStartTimeIdentity(storedIdentity) {
		currentStart, err := readLegacyProcessStartTime(p.PID)
		if err != nil {
			if !processExists(p.PID) {
				return false, nil
			}
			return false, fmt.Errorf("verify process %q identity: %w", p.ID, err)
		}
		if currentStart == storedIdentity {
			p.ProcessStartTime = currentIdentity
			return true, nil
		}
	}
	return false, fmt.Errorf("process %q identity changed; refusing to signal reused pid %d", p.ID, p.PID)
}

// legacyStartTimeLayout is the exact format older releases persisted: the
// verbatim output of "ps -o lstart=".
const legacyStartTimeLayout = "Mon Jan _2 15:04:05 2006"

func isLegacyStartTimeIdentity(identity string) bool {
	_, err := time.ParseInLocation(legacyStartTimeLayout, identity, time.Local)
	return err == nil
}

// readLegacyProcessStartTime reads the start time in the format older releases
// recorded. Legacy records are verified only by exact string comparison; any
// difference means the pid cannot be proven to belong to the record.
func readLegacyProcessStartTime(pid int) (string, error) {
	if pid <= 1 {
		return "", fmt.Errorf("invalid process id %d", pid)
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output()
	if err != nil {
		return "", fmt.Errorf("read start time for process %d: %w", pid, err)
	}
	started := strings.TrimSpace(string(out))
	if started == "" {
		return "", fmt.Errorf("process %d has no start time", pid)
	}
	return started, nil
}

const persistedProcessWatchInterval = 250 * time.Millisecond

// resumePersistedManagedProcesses restores exit observation for managed
// processes that outlived the manager which started them. PTY/stdin handles
// cannot be reconstructed, but terminal state and completion delivery can.
func (m *Manager) resumePersistedManagedProcesses() error {
	processes, err := m.List()
	if err != nil {
		return err
	}
	for _, p := range processes {
		if p.Lifecycle != LifecycleManaged || !isLiveStatus(p.Status) {
			continue
		}
		matched, matchErr := processMatchesRecord(&p)
		if matched && matchErr == nil {
			go m.watchPersistedManagedProcess(p.ID)
			continue
		}
		if _, err := m.settlePersistedManagedProcess(p.ID, matchErr); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) watchPersistedManagedProcess(id string) {
	ticker := time.NewTicker(persistedProcessWatchInterval)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		p, err := m.load(id)
		if err != nil || p.Lifecycle != LifecycleManaged || !isLiveStatus(p.Status) {
			m.mu.Unlock()
			return
		}
		matched, matchErr := processMatchesRecord(p)
		m.mu.Unlock()
		if matched && matchErr == nil {
			continue
		}
		_, _ = m.settlePersistedManagedProcess(id, matchErr)
		return
	}
}

func (m *Manager) settlePersistedManagedProcess(id string, observationErr error) (*Process, error) {
	m.mu.Lock()
	p, err := m.load(id)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if !isLiveStatus(p.Status) {
		m.mu.Unlock()
		return p, nil
	}
	requestedStop := p.Status == StatusStopping
	if observationErr != nil {
		p.Status = StatusFailed
		p.LastError = fmt.Sprintf("managed process could not be re-observed after restart: %v", observationErr)
	} else {
		p.Status = StatusStopped
		p.LastError = "managed process exit observed after restart; exit code unavailable"
	}
	p.ExitCode = -1
	p.StoppedAt = time.Now()
	p.UpdatedAt = p.StoppedAt
	p.TerminalCause = EventCauseNaturalExit
	if requestedStop {
		p.TerminalCause = EventCauseRequestedStop
	}
	if err := m.save(p); err != nil {
		m.mu.Unlock()
		return p, err
	}
	m.mu.Unlock()
	eventType := EventStopped
	if p.Status == StatusFailed {
		eventType = EventFailed
	}
	m.publish(Event{Type: eventType, Cause: p.TerminalCause, Process: *p})
	return p, nil
}

// PendingCompletions returns naturally exited processes whose model-facing
// completion obligation has not yet been acknowledged.
func (m *Manager) PendingCompletions() ([]Process, error) {
	processes, err := m.List()
	if err != nil {
		return nil, err
	}
	pending := make([]Process, 0)
	for _, p := range processes {
		if p.CompletionMode != CompletionModeDetached && p.TerminalCause == EventCauseNaturalExit &&
			(p.Status == StatusStopped || p.Status == StatusFailed) &&
			p.CompletionDeliveredAt.IsZero() {
			pending = append(pending, p)
		}
	}
	return pending, nil
}

func (m *Manager) CompletionPending(id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.load(id)
	if err != nil {
		return false, err
	}
	return p.CompletionMode != CompletionModeDetached && p.TerminalCause == EventCauseNaturalExit &&
		(p.Status == StatusStopped || p.Status == StatusFailed) &&
		p.CompletionDeliveredAt.IsZero(), nil
}

func (m *Manager) MarkCompletionDelivered(id, consumer string) (*Process, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.load(id)
	if err != nil {
		return nil, err
	}
	if p.CompletionMode == CompletionModeDetached || p.TerminalCause != EventCauseNaturalExit || (p.Status != StatusStopped && p.Status != StatusFailed) {
		return p, fmt.Errorf("process %q has no natural-exit completion to deliver", id)
	}
	if p.CompletionDeliveredAt.IsZero() {
		p.CompletionDeliveredAt = time.Now().UTC()
		p.CompletionConsumedBy = strings.TrimSpace(consumer)
		p.UpdatedAt = p.CompletionDeliveredAt
		if err := m.save(p); err != nil {
			return p, err
		}
	}
	return p, nil
}

func (m *Manager) CleanupSession() error {
	_, err := m.CleanupSessionWithResult()
	return err
}

func (m *Manager) CleanupSessionWithResult() (CleanupResult, error) {
	result := CleanupResult{}
	list, err := m.List()
	if err != nil {
		return result, err
	}
	for _, p := range list {
		if p.Lifecycle == LifecycleSession && (p.Status == StatusRunning || p.Status == StatusStarting || p.Status == StatusStopping) {
			stopped, err := m.Stop(p.ID)
			if err != nil {
				return result, err
			}
			if stopped != nil {
				result.Cleaned = append(result.Cleaned, *stopped)
				m.publish(Event{Type: EventCleanedUp, Process: *stopped})
			}
		}
	}
	return result, nil
}

func (m *Manager) Subscribe(ch chan<- Event) {
	if ch == nil {
		return
	}
	m.subMu.Lock()
	defer m.subMu.Unlock()
	m.subscribers = append(m.subscribers, ch)
}

func (m *Manager) Unsubscribe(ch chan<- Event) {
	if ch == nil {
		return
	}
	m.subMu.Lock()
	defer m.subMu.Unlock()
	next := m.subscribers[:0]
	for _, subscriber := range m.subscribers {
		if subscriber != ch {
			next = append(next, subscriber)
		}
	}
	m.subscribers = next
}

func (m *Manager) publish(event Event) {
	m.subMu.Lock()
	subs := append([]chan<- Event(nil), m.subscribers...)
	m.subMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (m *Manager) load(id string) (*Process, error) {
	path, err := processRecordPath(m.registryDir, id)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Process
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	if p.ID != id {
		return nil, fmt.Errorf("process record %q contains mismatched id %q", filepath.Base(path), p.ID)
	}
	return &p, nil
}

func (m *Manager) save(p *Process) error {
	path, err := processRecordPath(m.registryDir, p.ID)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return securefs.WriteFileAtomic(path, append(b, '\n'))
}

func processRecordPath(registryDir, id string) (string, error) {
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id || strings.ContainsAny(id, `/\`) {
		return "", fmt.Errorf("invalid process id %q", id)
	}
	return filepath.Join(registryDir, id+".json"), nil
}

func randomHex(n int) string { b := make([]byte, n); _, _ = rand.Read(b); return hex.EncodeToString(b) }
