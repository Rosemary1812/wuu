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
	requestHandler, err := newServerRequestHandler(opts)
	if err != nil {
		return nil, err
	}

	rt, err := runtime.NewSession(runtime.Options{
		RootDir:       rootDir,
		HomeDir:       homeDir,
		ConfigPath:    configPath,
		Config:        cfg,
		ProviderName:  opts.Provider,
		ModelOverride: opts.Model,
		NoTools:       opts.NoTools,
	})
	if err != nil {
		return nil, err
	}

	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()
	serverCtx, cancel := context.WithCancel(ctx)
	controller := &localAppServerController{
		rt:     rt,
		client: NewProtocolClientWithServerRequestHandler(serverCtx, serverOutR, serverInW, requestHandler),
		cancel: cancel,
		done:   make(chan error, 1),
		pipes:  []io.Closer{serverInR, serverInW, serverOutR, serverOutW},
	}
	go func() {
		err := appserver.RunStdio(serverCtx, rt, serverInR, serverOutW)
		controller.done <- err
	}()
	return controller, nil
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
		homeDir = ""
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
		if _, err := config.ApplyPermissionModePreset(&cfg.Agent, opts.PermissionMode); err != nil {
			return err
		}
	}
	if err := resolveApprovalsMode(&cfg.Agent, opts); err != nil {
		return err
	}
	if err := applyToolPolicyOverrides(&cfg.Agent.ToolPolicy, opts.AllowTools, opts.DenyTools); err != nil {
		return err
	}
	return nil
}

// resolveApprovalsMode maps exec's --approvals mode onto the agent
// approval policy. The default is auto: a delegated exec run flows -
// approval_policy resolves to never, so ask-classified actions (e.g.
// unclassified toolchain commands) execute the way full_access executes
// them, while destructive commands stay denied by the command policy and
// the tool layer's hard protections never turn off. on_request review
// flows are explicit opt-ins: --approvals strict/prompt, an approval
// handler or socket, or a configured auto_review reviewer.
func resolveApprovalsMode(agent *config.AgentConfig, opts Options) error {
	if agent == nil {
		return nil
	}
	mode, err := NormalizeApprovalsMode(opts.ApprovalsMode)
	if err != nil {
		return err
	}
	switch {
	case mode == ApprovalsModeStrict || mode == ApprovalsModePrompt:
		agent.ApprovalPolicy = config.ApprovalPolicyOnRequest
	case strings.TrimSpace(opts.ApprovalHandler) != "" || strings.TrimSpace(opts.ApprovalSocket) != "":
		agent.ApprovalPolicy = config.ApprovalPolicyOnRequest
	case mode == ApprovalsModeAuto:
		// explicit auto wins over any configured review flow
		agent.ApprovalPolicy = config.ApprovalPolicyNever
	case config.ResolveAgentPermissions(*agent).ApprovalsReviewer == config.ApprovalsReviewerAutoReview:
		// auto_review has a headless answerer (the guardian); keep the
		// configured review flow.
	default:
		agent.ApprovalPolicy = config.ApprovalPolicyNever
	}
	return nil
}

func applyToolPolicyOverrides(policy *config.ToolPolicyConfig, allowTools, denyTools []string) error {
	if policy == nil {
		return nil
	}
	allow := normalizedToolList(allowTools)
	deny := normalizedToolList(denyTools)
	if len(allow) == 0 && len(deny) == 0 {
		return nil
	}
	conflicts := map[string]bool{}
	for _, name := range allow {
		conflicts[name] = true
	}
	for _, name := range deny {
		if conflicts[name] {
			return fmt.Errorf("tool %q cannot be both allowed and denied", name)
		}
	}
	if policy.Tools == nil {
		policy.Tools = make(map[string]string)
	}
	for _, name := range allow {
		policy.Tools[name] = "allow"
	}
	for _, name := range deny {
		policy.Tools[name] = "deny"
	}
	return nil
}

func normalizedToolList(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
