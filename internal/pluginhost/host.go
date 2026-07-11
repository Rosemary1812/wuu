package pluginhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// Client is one initialized plugin runtime. Implementations may host plugins
// in subprocesses or in-process tests, but share the same typed wire contract.
type Client interface {
	ID() string
	Hooks() []Hook
	Invoke(context.Context, InvokeParams) (InvokeResult, error)
	Status() Status
	Close(context.Context) error
}

// Host invokes plugins in discovery order. A hook receives the output from the
// previous plugin, matching OpenCode's deterministic transform semantics.
type Host struct {
	mu      sync.RWMutex
	clients []Client
}

func New(clients ...Client) *Host {
	return &Host{clients: append([]Client(nil), clients...)}
}

// Failed preserves a plugin load failure in the runtime inventory without
// registering any interception points.
func Failed(id string, err error) Client {
	message := "plugin failed to start"
	if err != nil {
		message = err.Error()
	}
	return &failedClient{status: Status{ID: id, State: StateFailed, Error: message}}
}

func (h *Host) Add(client Client) {
	if h == nil || client == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients = append(h.clients, client)
}

// Run applies every plugin registered for hook to output. Output must be a
// non-nil pointer so each successful transform can become the next input.
func (h *Host) Run(ctx context.Context, hook Hook, input, output any) error {
	if h == nil {
		return nil
	}
	if !IsValidHook(hook) {
		return fmt.Errorf("unknown plugin hook %q", hook)
	}
	if output == nil {
		return fmt.Errorf("plugin hook %q requires output", hook)
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal plugin hook %q input: %w", hook, err)
	}
	outputJSON, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("marshal plugin hook %q output: %w", hook, err)
	}

	h.mu.RLock()
	clients := append([]Client(nil), h.clients...)
	h.mu.RUnlock()
	for _, client := range clients {
		if !hasHook(client.Hooks(), hook) {
			continue
		}
		result, invokeErr := client.Invoke(ctx, InvokeParams{
			Hook:   hook,
			Input:  inputJSON,
			Output: outputJSON,
		})
		if invokeErr != nil {
			return fmt.Errorf("plugin %q hook %q: %w", client.ID(), hook, invokeErr)
		}
		if len(result.Output) == 0 {
			return fmt.Errorf("plugin %q hook %q returned empty output", client.ID(), hook)
		}
		if err := json.Unmarshal(result.Output, output); err != nil {
			return fmt.Errorf("plugin %q hook %q returned invalid output: %w", client.ID(), hook, err)
		}
		outputJSON = append(outputJSON[:0], result.Output...)
	}
	return nil
}

func (h *Host) Statuses() []Status {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	clients := append([]Client(nil), h.clients...)
	h.mu.RUnlock()
	out := make([]Status, 0, len(clients))
	for _, client := range clients {
		out = append(out, client.Status())
	}
	return out
}

func (h *Host) Close(ctx context.Context) error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	clients := append([]Client(nil), h.clients...)
	h.clients = nil
	h.mu.Unlock()

	var firstErr error
	for i := len(clients) - 1; i >= 0; i-- {
		if err := clients[i].Close(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close plugin %q: %w", clients[i].ID(), err)
		}
	}
	return firstErr
}

func hasHook(hooks []Hook, target Hook) bool {
	for _, hook := range hooks {
		if hook == target {
			return true
		}
	}
	return false
}

type failedClient struct{ status Status }

func (c *failedClient) ID() string    { return c.status.ID }
func (c *failedClient) Hooks() []Hook { return nil }
func (c *failedClient) Invoke(context.Context, InvokeParams) (InvokeResult, error) {
	return InvokeResult{}, errors.New(c.status.Error)
}
func (c *failedClient) Status() Status              { return c.status }
func (c *failedClient) Close(context.Context) error { return nil }
