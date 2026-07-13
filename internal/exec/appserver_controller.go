package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
		NoTools:        opts.NoTools,
	})
	if err != nil {
		return nil, err
	}
	return newLocalControllerForRuntime(ctx, rt), nil
}

// newLocalControllerForRuntime wires an in-process app server (over pipes)
// around an already-built runtime.Session. NewLocalAppServerController builds
// the runtime from config; end-to-end tests supply a fake-provider runtime so
// they can drive the real group/reply/task handlers deterministically.
func newLocalControllerForRuntime(ctx context.Context, rt *runtime.Session) *localAppServerController {
	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()
	serverCtx, cancel := context.WithCancel(ctx)
	controller := &localAppServerController{
		rt:     rt,
		client: NewProtocolClientWithServerRequestHandler(serverCtx, serverOutR, serverInW, nil),
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

func (c *localAppServerController) StartParticipantTurn(ctx context.Context, params appserver.ParticipantStartParams) (appserver.Agent, error) {
	var result appserver.ParticipantStartResult
	err := c.client.Call(ctx, appserver.MethodParticipantStart, params, &result)
	return result.Agent, err
}

func (c *localAppServerController) Call(ctx context.Context, method string, params, result any) error {
	return c.client.Call(ctx, method, params, result)
}

func (c *localAppServerController) Interrupt(ctx context.Context, threadID string) error {
	var result appserver.OKResult
	return c.client.Call(ctx, appserver.MethodTurnInterrupt, appserver.TurnInterruptParams{ThreadID: threadID}, &result)
}

func (c *localAppServerController) Shutdown(ctx context.Context) error {
	if c.cancel != nil {
		defer c.cancel()
	}
	var result appserver.OKResult
	err := c.client.Call(ctx, appserver.MethodShutdown, nil, &result)
	for _, pipe := range c.pipes {
		_ = pipe.Close()
	}
	if c.rt != nil {
		_, _ = c.rt.Cleanup()
	}
	select {
	case runErr := <-c.done:
		if err == nil && runErr != nil && !errors.Is(runErr, io.ErrClosedPipe) {
			err = runErr
		}
	default:
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
	return nil
}
