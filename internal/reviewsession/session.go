package reviewsession

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/modelvariant"
	"github.com/blueberrycongee/wuu/internal/providers"
)

const (
	DefaultTimeout   = 30 * time.Second
	DefaultMaxTokens = 512

	PermissionProfileReadOnly = "read_only"
	ApprovalPolicyNever       = "never"
)

type Outcome string

const (
	OutcomeCompleted Outcome = "completed"
	OutcomeTimedOut  Outcome = "timed_out"
	OutcomeCanceled  Outcome = "canceled"
	OutcomeFailed    Outcome = "failed"
)

type Boundary struct {
	PermissionProfile string `json:"permission_profile"`
	ApprovalPolicy    string `json:"approval_policy"`
	Tools             bool   `json:"tools"`
	MCP               bool   `json:"mcp"`
	Hooks             bool   `json:"hooks"`
	Plugins           bool   `json:"plugins"`
	Skills            bool   `json:"skills"`
	MemoryWrites      bool   `json:"memory_writes"`
	DurableWrites     bool   `json:"durable_writes"`
}

type Config struct {
	Client          providers.Client
	Model           string
	Role            string
	ParentSessionID string
	ParentTurnID    string
	Timeout         time.Duration
	MaxTokens       int
	Effort          string
	ProviderOptions map[string]any
	Boundary        Boundary
}

type Session struct {
	client          providers.Client
	model           string
	role            string
	parentSessionID string
	parentTurnID    string
	timeout         time.Duration
	maxTokens       int
	effort          string
	providerOptions map[string]any
	boundary        Boundary
}

type Request struct {
	SystemPrompt    string
	Prompt          string
	Messages        []providers.ChatMessage
	MaxTokens       int
	Effort          string
	ProviderOptions map[string]any
}

type Result struct {
	Outcome         Outcome
	Content         string
	Error           string
	ErrorKind       string
	Model           string
	Role            string
	ParentSessionID string
	ParentTurnID    string
	Boundary        Boundary
	StartedAt       time.Time
	CompletedAt     time.Time
	DurationMS      int64
}

func New(cfg Config) (*Session, error) {
	if cfg.Client == nil {
		return nil, errors.New("review session client is required")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, errors.New("review session model is required")
	}
	boundary := cfg.Boundary
	if boundary == (Boundary{}) {
		boundary = RestrictedBoundary()
	}
	if err := boundary.ValidateRestricted(); err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	return &Session{
		client:          cfg.Client,
		model:           model,
		role:            strings.TrimSpace(cfg.Role),
		parentSessionID: strings.TrimSpace(cfg.ParentSessionID),
		parentTurnID:    strings.TrimSpace(cfg.ParentTurnID),
		timeout:         timeout,
		maxTokens:       maxTokens,
		effort:          strings.TrimSpace(cfg.Effort),
		providerOptions: modelvariant.CloneOptions(cfg.ProviderOptions),
		boundary:        boundary,
	}, nil
}

func RestrictedBoundary() Boundary {
	return Boundary{
		PermissionProfile: PermissionProfileReadOnly,
		ApprovalPolicy:    ApprovalPolicyNever,
	}
}

func (b Boundary) ValidateRestricted() error {
	if strings.TrimSpace(b.PermissionProfile) != PermissionProfileReadOnly {
		return fmt.Errorf("review session permission_profile must be %q", PermissionProfileReadOnly)
	}
	if strings.TrimSpace(b.ApprovalPolicy) != ApprovalPolicyNever {
		return fmt.Errorf("review session approval_policy must be %q", ApprovalPolicyNever)
	}
	if b.Tools || b.MCP || b.Hooks || b.Plugins || b.Skills || b.MemoryWrites || b.DurableWrites {
		return errors.New("review session boundary must disable tools, MCP, hooks, plugins, skills, memory writes, and durable writes")
	}
	return nil
}

func (s *Session) Boundary() Boundary {
	if s == nil {
		return Boundary{}
	}
	return s.boundary
}

func (s *Session) Model() string {
	if s == nil {
		return ""
	}
	return s.model
}

func (s *Session) Role() string {
	if s == nil {
		return ""
	}
	return s.role
}

func (s *Session) Run(ctx context.Context, req Request) (result Result) {
	startedAt := time.Now().UTC()
	result = Result{
		Outcome:         OutcomeFailed,
		Model:           s.model,
		Role:            s.role,
		ParentSessionID: s.parentSessionID,
		ParentTurnID:    s.parentTurnID,
		Boundary:        s.boundary,
		StartedAt:       startedAt,
	}
	defer func() {
		result.CompletedAt = time.Now().UTC()
		result.DurationMS = result.CompletedAt.Sub(startedAt).Milliseconds()
	}()

	if s == nil || s.client == nil {
		return result.withError(OutcomeFailed, "invalid_config", "review session is not configured")
	}
	messages := buildMessages(req)
	if len(messages) == 0 {
		return result.withError(OutcomeFailed, "invalid_request", "review session request requires messages or prompt")
	}
	timeout := s.timeout
	callCtx := ctx
	if callCtx == nil {
		callCtx = context.Background()
	}
	var cancel context.CancelFunc
	callCtx, cancel = context.WithTimeout(callCtx, timeout)
	defer cancel()

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = s.maxTokens
	}
	effort := strings.TrimSpace(req.Effort)
	if effort == "" {
		effort = s.effort
	}
	options := modelvariant.CloneOptions(s.providerOptions)
	if len(req.ProviderOptions) > 0 {
		options = modelvariant.CloneOptions(req.ProviderOptions)
	}

	resp, err := s.client.Chat(callCtx, providers.ChatRequest{
		Model:           s.model,
		Messages:        messages,
		MaxTokens:       maxTokens,
		Effort:          effort,
		ProviderOptions: options,
	})
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return result.withError(OutcomeTimedOut, "timed_out", err.Error())
		case errors.Is(err, context.Canceled):
			return result.withError(OutcomeCanceled, "canceled", err.Error())
		default:
			return result.withError(OutcomeFailed, "provider_error", err.Error())
		}
	}
	result.Outcome = OutcomeCompleted
	result.Content = resp.Content
	return result
}

func buildMessages(req Request) []providers.ChatMessage {
	messages := append([]providers.ChatMessage(nil), req.Messages...)
	if strings.TrimSpace(req.SystemPrompt) != "" {
		messages = append([]providers.ChatMessage{{Role: "system", Content: strings.TrimSpace(req.SystemPrompt)}}, messages...)
	}
	if strings.TrimSpace(req.Prompt) != "" {
		messages = append(messages, providers.ChatMessage{Role: "user", Content: strings.TrimSpace(req.Prompt)})
	}
	return messages
}

func (r Result) withError(outcome Outcome, kind, message string) Result {
	r.Outcome = outcome
	r.ErrorKind = strings.TrimSpace(kind)
	r.Error = strings.TrimSpace(message)
	return r
}
