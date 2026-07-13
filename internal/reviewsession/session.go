package reviewsession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/modelvariant"
	"github.com/blueberrycongee/wuu/internal/providers"
)

const (
	DefaultTimeout     = 30 * time.Second
	DefaultMaxTokens   = 512
	PermissionModeRead = "read_only"
)

type Outcome string

const (
	OutcomeCompleted Outcome = "completed"
	OutcomeTimedOut  Outcome = "timed_out"
	OutcomeCanceled  Outcome = "canceled"
	OutcomeFailed    Outcome = "failed"
)

type Boundary struct {
	PermissionMode string `json:"permission_mode"`
	Tools          bool   `json:"tools"`
	MCP            bool   `json:"mcp"`
	Hooks          bool   `json:"hooks"`
	Plugins        bool   `json:"plugins"`
	Skills         bool   `json:"skills"`
	MemoryWrites   bool   `json:"memory_writes"`
	DurableWrites  bool   `json:"durable_writes"`
}

type Config struct {
	Client           providers.Client
	Model            string
	Role             string
	ParentSessionID  string
	ParentTurnID     string
	Timeout          time.Duration
	MaxTokens        int
	Effort           string
	ProviderOptions  map[string]any
	Boundary         Boundary
	InferenceJournal providers.InferenceJournal
}

type ForkOptions struct {
	Role            string
	ParentSessionID string
	ParentTurnID    string
	Timeout         time.Duration
	MaxTokens       int
	Effort          string
	ProviderOptions map[string]any
}

type Session struct {
	client           providers.Client
	model            string
	role             string
	parentSessionID  string
	parentTurnID     string
	timeout          time.Duration
	maxTokens        int
	effort           string
	providerOptions  map[string]any
	boundary         Boundary
	inferenceJournal providers.InferenceJournal
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
	Outcome            Outcome
	Content            string
	Error              string
	ErrorKind          string
	RequestFingerprint string
	Model              string
	Role               string
	ParentSessionID    string
	ParentTurnID       string
	Boundary           Boundary
	StartedAt          time.Time
	CompletedAt        time.Time
	DurationMS         int64
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
		client:           cfg.Client,
		model:            model,
		role:             strings.TrimSpace(cfg.Role),
		parentSessionID:  strings.TrimSpace(cfg.ParentSessionID),
		parentTurnID:     strings.TrimSpace(cfg.ParentTurnID),
		timeout:          timeout,
		maxTokens:        maxTokens,
		effort:           strings.TrimSpace(cfg.Effort),
		providerOptions:  modelvariant.CloneOptions(cfg.ProviderOptions),
		boundary:         boundary,
		inferenceJournal: cfg.InferenceJournal,
	}, nil
}

func (s *Session) Fork(opts ForkOptions) (*Session, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("review session is not configured")
	}
	fork := *s
	if role := strings.TrimSpace(opts.Role); role != "" {
		fork.role = role
	}
	if parentSessionID := strings.TrimSpace(opts.ParentSessionID); parentSessionID != "" {
		fork.parentSessionID = parentSessionID
	}
	if parentTurnID := strings.TrimSpace(opts.ParentTurnID); parentTurnID != "" {
		fork.parentTurnID = parentTurnID
	}
	if opts.Timeout > 0 {
		fork.timeout = opts.Timeout
	}
	if opts.MaxTokens > 0 {
		fork.maxTokens = opts.MaxTokens
	}
	if effort := strings.TrimSpace(opts.Effort); effort != "" {
		fork.effort = effort
	}
	if len(opts.ProviderOptions) > 0 {
		fork.providerOptions = modelvariant.CloneOptions(opts.ProviderOptions)
	} else {
		fork.providerOptions = modelvariant.CloneOptions(s.providerOptions)
	}
	return &fork, nil
}

func RestrictedBoundary() Boundary {
	return Boundary{
		PermissionMode: PermissionModeRead,
	}
}

func (b Boundary) ValidateRestricted() error {
	if strings.TrimSpace(b.PermissionMode) != PermissionModeRead {
		return fmt.Errorf("review session permission_mode must be %q", PermissionModeRead)
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
		Outcome:   OutcomeFailed,
		StartedAt: startedAt,
	}
	defer func() {
		result.CompletedAt = time.Now().UTC()
		result.DurationMS = result.CompletedAt.Sub(startedAt).Milliseconds()
	}()

	if s == nil || s.client == nil {
		return result.withError(OutcomeFailed, "invalid_config", "review session is not configured")
	}
	result.Model = s.model
	result.Role = s.role
	result.ParentSessionID = s.parentSessionID
	result.ParentTurnID = s.parentTurnID
	result.Boundary = s.boundary

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
	callCtx = providers.WithInferenceJournal(callCtx, s.inferenceJournal)

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
	result.RequestFingerprint = reviewRequestFingerprint(reviewRequestFingerprintInput{
		Model:           s.model,
		Role:            s.role,
		Boundary:        s.boundary,
		Messages:        messages,
		MaxTokens:       maxTokens,
		Effort:          effort,
		ProviderOptions: options,
	})

	resp, err := providers.ExecuteChat(callCtx, s.client, providers.ChatRequest{
		Model:           s.model,
		Messages:        messages,
		MaxTokens:       maxTokens,
		Effort:          effort,
		ProviderOptions: options,
	}, providers.InferenceOperationReview, providers.InferenceProfileBackgroundAgent)
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
	messages := providers.CloneChatMessages(req.Messages)
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

type reviewRequestFingerprintInput struct {
	Model           string                  `json:"model"`
	Role            string                  `json:"role,omitempty"`
	Boundary        Boundary                `json:"boundary"`
	Messages        []providers.ChatMessage `json:"messages"`
	MaxTokens       int                     `json:"max_tokens,omitempty"`
	Effort          string                  `json:"effort,omitempty"`
	ProviderOptions map[string]any          `json:"provider_options,omitempty"`
}

func reviewRequestFingerprint(input reviewRequestFingerprintInput) string {
	data, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
