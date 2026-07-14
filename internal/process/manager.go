package process

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/blueberrycongee/wuu/internal/statepath"
)

type OwnerKind string
type Lifecycle string
type Status string
type EventType string

const (
	EventStarted   EventType = "started"
	EventUpdated   EventType = "updated"
	EventFailed    EventType = "failed"
	EventStopped   EventType = "stopped"
	EventCleanedUp EventType = "cleaned_up"

	OwnerMainAgent OwnerKind = "main_agent"
	OwnerSubagent  OwnerKind = "subagent"

	LifecycleSession Lifecycle = "session"
	LifecycleManaged Lifecycle = "managed"

	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopping Status = "stopping"
	StatusStopped  Status = "stopped"
	StatusFailed   Status = "failed"
)

type Process struct {
	Action            string    `json:"action,omitempty"`
	ID                string    `json:"id"`
	OwnerKind         OwnerKind `json:"owner_kind"`
	OwnerID           string    `json:"owner_id"`
	Lifecycle         Lifecycle `json:"lifecycle"`
	Status            Status    `json:"status"`
	PID               int       `json:"pid"`
	PGID              int       `json:"pgid"`
	ProcessStartTime  string    `json:"process_start_time,omitempty"`
	TTY               bool      `json:"tty,omitempty"`
	LogPath           string    `json:"log_path"`
	Command           string    `json:"command"`
	CWD               string    `json:"cwd"`
	PreviewURLs       []string  `json:"preview_urls,omitempty"`
	PrimaryPreviewURL string    `json:"primary_preview_url,omitempty"`
	StartedAt         time.Time `json:"started_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	StoppedAt         time.Time `json:"stopped_at,omitempty"`
	ExitCode          int       `json:"exit_code,omitempty"`
	LastError         string    `json:"last_error,omitempty"`
}

type StartOptions struct {
	Command               string
	CWD                   string
	OwnerKind             OwnerKind
	OwnerID               string
	Lifecycle             Lifecycle
	TTY                   bool
	AllowOutsideWorkspace bool
}

type Event struct {
	Type    EventType
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
	cwd, err := resolveStartCWD(m.rootDir, opt.CWD, opt.AllowOutsideWorkspace)
	if err != nil {
		return nil, err
	}
	id := "proc-" + randomHex(4)
	p := &Process{ID: id, OwnerKind: opt.OwnerKind, OwnerID: opt.OwnerID, Lifecycle: opt.Lifecycle, Status: StatusStarting, Command: opt.Command, CWD: cwd, TTY: opt.TTY, LogPath: filepath.Join(m.logDir, id+".log"), StartedAt: time.Now(), UpdatedAt: time.Now(), ExitCode: -1}
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
	cmd := managedCommand(opt.Command, cwd)
	var stdin io.WriteCloser
	var ptyFile *os.File
	if opt.TTY {
		ptyFile, err = pty.StartWithAttrs(cmd, &pty.Winsize{Rows: 24, Cols: 80}, &syscall.SysProcAttr{Setsid: true, Setctty: true})
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
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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
	if pgid, err := syscall.Getpgid(p.PID); err == nil {
		p.PGID = pgid
	} else {
		p.PGID = p.PID
	}
	p.ProcessStartTime, err = readProcessStartTime(p.PID)
	if err != nil {
		if p.PGID > 1 {
			_ = syscall.Kill(-p.PGID, syscall.SIGKILL)
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
	m.handles[id] = &processHandle{stdin: stdin}
	m.publish(Event{Type: EventStarted, Process: *p})
	if opt.TTY {
		go m.waitPTY(id, cmd, logf, ptyFile)
	} else {
		go m.wait(id, cmd, logf)
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
	rel, err := filepath.Rel(root, evaluated)
	if err != nil {
		return "", fmt.Errorf("resolve working directory %q against workspace root %q: %w", evaluated, root, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("working directory %q escapes workspace %q", evaluated, root)
	}
	return evaluated, nil
}

func managedCommand(command, cwd string) *exec.Cmd {
	cmd := exec.Command("bash", "-lc", command)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	return cmd
}

func readProcessStartTime(pid int) (string, error) {
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
	if p.Status == StatusStopping {
		p.Status = StatusStopped
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
	eventType := EventStopped
	if p.Status == StatusFailed {
		eventType = EventFailed
	}
	m.publish(Event{Type: eventType, Process: *p})
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
		if err == nil {
			var p Process
			if json.Unmarshal(b, &p) == nil {
				out = append(out, p)
			}
		}
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
	if p.Status == StatusStopped || p.Status == StatusFailed {
		m.mu.Unlock()
		return p, nil
	}
	running, err := processMatchesRecord(p)
	if err != nil {
		m.mu.Unlock()
		return p, err
	}
	if !running {
		delete(m.handles, id)
		p.Status = StatusStopped
		p.StoppedAt = time.Now()
		p.UpdatedAt = p.StoppedAt
		if err := m.save(p); err != nil {
			m.mu.Unlock()
			return p, err
		}
		m.mu.Unlock()
		m.publish(Event{Type: EventStopped, Process: *p})
		return p, nil
	}
	p.Status = StatusStopping
	p.UpdatedAt = time.Now()
	_ = m.save(p)
	m.mu.Unlock()
	pgid := p.PGID
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return p, fmt.Errorf("terminate process group %d: %w", pgid, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cur, err := m.load(id)
		if err != nil {
			return nil, err
		}
		if cur.Status == StatusStopped || cur.Status == StatusFailed {
			return cur, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return p, fmt.Errorf("kill process group %d: %w", pgid, err)
	}
	time.Sleep(100 * time.Millisecond)
	cur, err := m.load(id)
	if err != nil {
		return nil, err
	}
	return cur, nil
}

func processMatchesRecord(p *Process) (bool, error) {
	if p.PID <= 1 || p.PGID <= 1 {
		return false, fmt.Errorf("process %q has unsafe pid/pgid %d/%d", p.ID, p.PID, p.PGID)
	}
	if strings.TrimSpace(p.ProcessStartTime) == "" {
		return false, fmt.Errorf("process %q has no recorded start time; refusing to signal pid %d", p.ID, p.PID)
	}
	currentStart, err := readProcessStartTime(p.PID)
	if err != nil {
		if !processExists(p.PID) {
			return false, nil
		}
		return false, fmt.Errorf("verify process %q identity: %w", p.ID, err)
	}
	if currentStart != p.ProcessStartTime {
		return false, fmt.Errorf("process %q identity changed; refusing to signal reused pid %d", p.ID, p.PID)
	}
	currentPGID, err := syscall.Getpgid(p.PID)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return false, nil
		}
		return false, fmt.Errorf("verify process %q group: %w", p.ID, err)
	}
	if currentPGID != p.PGID {
		return false, fmt.Errorf("process %q group changed from %d to %d; refusing to signal it", p.ID, p.PGID, currentPGID)
	}
	return true, nil
}

func processExists(pid int) bool {
	if pid <= 1 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
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
	m.subMu.Lock()
	defer m.subMu.Unlock()
	m.subscribers = append(m.subscribers, ch)
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
	b, err := os.ReadFile(filepath.Join(m.registryDir, id+".json"))
	if err != nil {
		return nil, err
	}
	var p Process
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
func (m *Manager) save(p *Process) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.registryDir, p.ID+".json"), append(b, '\n'), 0o644)
}
func randomHex(n int) string { b := make([]byte, n); _, _ = rand.Read(b); return hex.EncodeToString(b) }
