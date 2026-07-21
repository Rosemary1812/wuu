package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/appserver"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/runtime"
)

type localAppServerController struct {
	rt     *runtime.Session
	client *ProtocolClient
	cancel context.CancelFunc
	done   chan error
	pipes  []io.Closer
}

func NewLocalAppServerController(ctx context.Context, opts Options) (Controller, error) {
	rootDir, err := resolveWorkdir(opts.Workdir)
	if err != nil {
		return nil, err
	}
	homeDir := os.Getenv("HOME")
	cfg, configPath, err := loadExecConfig(rootDir, homeDir, opts)
	if err != nil {
		return nil, err
	}
	if err := applyConfigOverrides(&cfg, opts); err != nil {
		return nil, err
	}
	configLoadMode := runtime.ConfigLoadNormal
	if strings.TrimSpace(opts.ConfigPath) != "" {
		configLoadMode = runtime.ConfigLoadFile
	} else if opts.IgnoreUserConfig {
		configLoadMode = runtime.ConfigLoadProject
	}
	rt, err := runtime.NewSession(runtime.Options{
		RootDir:        rootDir,
		HomeDir:        homeDir,
		ConfigPath:     configPath,
		ConfigLoadMode: configLoadMode,
		Config:         cfg,
		ProviderName:   opts.Provider,
		ModelOverride:  opts.Model,
		// Only a --permission-mode flag actually passed to exec becomes the
		// process-scoped override that beats a resumed session's pinned mode.
		PermissionModeExplicit: strings.TrimSpace(opts.PermissionMode) != "",
		NoTools:                opts.NoTools,
	})
	if err != nil {
		return nil, err
	}
	return newLocalControllerForRuntime(ctx, rt), nil
}

// newLocalControllerForRuntime wires an in-process app server (over pipes)
// around an already-built runtime.Session. NewLocalAppServerController builds
// the runtime from config; end-to-end tests can supply a fake-provider runtime.
func newLocalControllerForRuntime(ctx context.Context, rt *runtime.Session) *localAppServerController {
	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()
	serverCtx, cancel := context.WithCancel(ctx)
	controller := &localAppServerController{
		rt:     rt,
		client: NewProtocolClient(serverOutR, serverInW),
		cancel: cancel,
		done:   make(chan error, 1),
		pipes:  []io.Closer{serverInR, serverInW, serverOutR, serverOutW},
	}
	go func() {
		err := appserver.RunStdio(serverCtx, rt, serverInR, serverOutW)
		controller.done <- err
	}()
	return controller
}

func loadExecConfig(rootDir, homeDir string, opts Options) (config.Config, string, error) {
	if strings.TrimSpace(opts.ConfigPath) != "" {
		path := strings.TrimSpace(opts.ConfigPath)
		if !filepath.IsAbs(path) {
			path = filepath.Join(rootDir, path)
		}
		return config.LoadPath(path)
	}
	if opts.IgnoreUserConfig {
		return config.LoadProjectConfig(rootDir)
	}
	return config.LoadFrom(rootDir, homeDir)
}

func (c *localAppServerController) Initialize(ctx context.Context) (appserver.InitializeResult, error) {
	var result appserver.InitializeResult
	err := c.client.Call(ctx, appserver.MethodInitialize, nil, &result)
	return result, err
}

func (c *localAppServerController) StartThread(ctx context.Context, ephemeral bool) (appserver.Thread, error) {
	var result appserver.ThreadStartResult
	params := appserver.ThreadStartParams{Ephemeral: ephemeral}
	err := c.client.Call(ctx, appserver.MethodThreadStart, params, &result)
	return result.Thread, err
}

func (c *localAppServerController) ResumeThread(ctx context.Context, threadID string) (appserver.Thread, error) {
	var result appserver.ThreadResumeResult
	params := appserver.ThreadResumeParams{SessionID: strings.TrimSpace(threadID)}
	err := c.client.Call(ctx, appserver.MethodThreadResume, params, &result)
	return result.Thread, err
}

func (c *localAppServerController) ForkThread(ctx context.Context, threadID string) (appserver.Thread, error) {
	var result appserver.ThreadForkResult
	params := appserver.ThreadForkParams{ThreadID: strings.TrimSpace(threadID)}
	err := c.client.Call(ctx, appserver.MethodThreadFork, params, &result)
	return result.Thread, err
}

func (c *localAppServerController) StartTurn(ctx context.Context, threadID string, input TurnInput) (appserver.Turn, error) {
	var result appserver.TurnStartResult
	params := appserver.TurnStartParams{
		ThreadID: threadID,
		Prompt:   input.Prompt,
		Images:   append([]appserver.TurnStartImage(nil), input.Images...),
		Files:    append([]appserver.TurnStartFile(nil), input.Files...),
	}
	err := c.client.Call(ctx, appserver.MethodTurnStart, params, &result)
	return result.Turn, err
}

func (c *localAppServerController) StartRun(ctx context.Context, params appserver.RunStartParams) (appserver.Run, error) {
	var result appserver.RunStartResult
	err := c.client.Call(ctx, appserver.MethodRunStart, params, &result)
	return result.Run, err
}

func (c *localAppServerController) ReadRun(ctx context.Context, runID string) (appserver.RunView, error) {
	var result appserver.RunReadResult
	err := c.client.Call(ctx, appserver.MethodRunRead, appserver.RunReadParams{RunID: strings.TrimSpace(runID)}, &result)
	return result, err
}

func (c *localAppServerController) InterruptRun(ctx context.Context, runID, reason string) (appserver.Run, error) {
	var result appserver.RunInterruptResult
	err := c.client.Call(ctx, appserver.MethodRunInterrupt, appserver.RunInterruptParams{RunID: strings.TrimSpace(runID), Reason: strings.TrimSpace(reason)}, &result)
	return result.Run, err
}

func (c *localAppServerController) Interrupt(ctx context.Context, threadID string) error {
	var result appserver.OKResult
	return c.client.Call(ctx, appserver.MethodTurnInterrupt, appserver.TurnInterruptParams{ThreadID: threadID}, &result)
}

// shutdownFallbackTimeout bounds Shutdown when the caller's context carries
// no deadline, so a wedged turn can never hang shutdown forever.
const shutdownFallbackTimeout = 10 * time.Second

func (c *localAppServerController) Shutdown(ctx context.Context) error {
	if c.cancel != nil {
		defer c.cancel()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, shutdownFallbackTimeout)
		defer cancel()
	}
	var result appserver.OKResult
	err := c.client.Call(ctx, appserver.MethodShutdown, nil, &result)
	for _, pipe := range c.pipes {
		_ = pipe.Close()
	}
	if c.done != nil {
		select {
		case runErr := <-c.done:
			if err == nil && runErr != nil && !errors.Is(runErr, io.ErrClosedPipe) {
				err = runErr
			}
		case <-ctx.Done():
			// The run loop has not finished draining its turns and worker
			// finalizers; closing the shared runtime under it would race, so
			// surface the timeout instead of cleaning up.
			return errors.Join(err, fmt.Errorf("app server run loop did not exit before shutdown deadline: %w", ctx.Err()))
		}
	}
	// RunStdio synchronously drains app-server-owned turns and worker
	// finalizers. Only then is it safe to close the shared runtime resources
	// those terminal paths still use.
	if c.rt != nil {
		_, _ = c.rt.Cleanup()
	}
	return err
}

func (c *localAppServerController) Notifications() <-chan Notification {
	return c.client.Notifications()
}

func resolveWorkdir(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get current directory: %w", err)
		}
		return cwd, nil
	}
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("resolve workdir: %w", err)
	}
	return abs, nil
}

func applyConfigOverrides(cfg *config.Config, opts Options) error {
	if cfg == nil {
		return nil
	}
	if strings.TrimSpace(opts.Effort) != "" {
		cfg.Agent.Effort = strings.TrimSpace(opts.Effort)
		if strings.TrimSpace(opts.Variant) == "" {
			cfg.Agent.Variant = ""
		}
	}
	if strings.TrimSpace(opts.Variant) != "" {
		cfg.Agent.Variant = strings.TrimSpace(opts.Variant)
	}
	if strings.TrimSpace(opts.AgentProfile) != "" {
		cfg.Agent.Name = strings.TrimSpace(opts.AgentProfile)
	}
	if opts.MaxTurns < 0 {
		return errors.New("max turns must be non-negative")
	}
	if opts.MaxTurns > 0 {
		cfg.Agent.MaxSteps = opts.MaxTurns
	}
	if strings.TrimSpace(opts.PermissionMode) != "" {
		cfg.Agent.PermissionMode = config.NormalizePermissionMode(opts.PermissionMode)
	}
	if opts.Ultra {
		cfg.Agent.UltraMode = true
	}
	return nil
}
