package providers

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type ProviderScope string

var providerScopeHMACKey = newProviderScopeHMACKey()

// NewProviderScope creates a stable process-local rate-limit domain without
// retaining or exposing the credential. A keyed HMAC prevents offline API-key
// guessing from a leaked diagnostic scope.
func NewProviderScope(origin, credential, organization string) ProviderScope {
	normalizedOrigin, originIdentity := normalizeProviderOrigin(origin)
	mac := hmac.New(sha256.New, providerScopeHMACKey)
	_, _ = mac.Write([]byte(originIdentity))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(credential))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(organization))
	digest := mac.Sum(nil)
	return ProviderScope(normalizedOrigin + "#" + hex.EncodeToString(digest[:12]))
}

func normalizeProviderOrigin(raw string) (display string, identity string) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err == nil && parsed.Scheme != "" && parsed.Hostname() != "" {
		scheme := strings.ToLower(parsed.Scheme)
		hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
		port := parsed.Port()
		if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
			port = ""
		}
		host := hostname
		if strings.Contains(hostname, ":") {
			host = "[" + hostname + "]"
		}
		if port != "" {
			host = net.JoinHostPort(hostname, port)
		}
		origin := scheme + "://" + host
		return origin, origin
	}
	if trimmed == "" {
		trimmed = "unknown-provider"
	}
	// Invalid endpoint text remains inside the keyed digest. Diagnostics get
	// a safe label instead of accidentally exposing userinfo or query tokens.
	return "unknown-provider", trimmed
}

func newProviderScopeHMACKey() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("providers: generate provider-scope HMAC key: " + err.Error())
	}
	return key
}

type ProviderPriority int

const (
	ProviderPriorityBestEffort  ProviderPriority = 10
	ProviderPriorityBackground  ProviderPriority = 20
	ProviderPriorityInteractive ProviderPriority = 30
	ProviderPriorityCritical    ProviderPriority = 40
)

func ProviderPriorityForProfile(profile InferenceWorkloadProfile) ProviderPriority {
	switch profile {
	case InferenceProfileContinuationCritical:
		return ProviderPriorityCritical
	case InferenceProfileInteractive:
		return ProviderPriorityInteractive
	case InferenceProfileBackgroundAgent:
		return ProviderPriorityBackground
	case InferenceProfileBestEffort:
		return ProviderPriorityBestEffort
	default:
		return ProviderPriorityInteractive
	}
}

type ProviderCoordinatorConfig struct {
	MaxInFlight               int
	ReservedInteractiveSlots  int
	CircuitFailureThreshold   int
	CircuitOpenDuration       time.Duration
	DefaultRateLimitDelay     time.Duration
	PriorityAgingInterval     time.Duration
	RecoveryAdmissionInterval time.Duration
}

func DefaultProviderCoordinatorConfig() ProviderCoordinatorConfig {
	return ProviderCoordinatorConfig{
		// Wuu currently permits five subagents plus the foreground turn. Keep
		// that established aggregate capacity while giving the queue control
		// over which workload receives it.
		MaxInFlight:               6,
		ReservedInteractiveSlots:  1,
		CircuitFailureThreshold:   5,
		CircuitOpenDuration:       30 * time.Second,
		DefaultRateLimitDelay:     2 * time.Second,
		PriorityAgingInterval:     10 * time.Second,
		RecoveryAdmissionInterval: 250 * time.Millisecond,
	}
}

func normalizeProviderCoordinatorConfig(cfg ProviderCoordinatorConfig) ProviderCoordinatorConfig {
	defaults := DefaultProviderCoordinatorConfig()
	if cfg.MaxInFlight <= 0 {
		cfg.MaxInFlight = defaults.MaxInFlight
	}
	if cfg.ReservedInteractiveSlots == 0 {
		cfg.ReservedInteractiveSlots = defaults.ReservedInteractiveSlots
	} else if cfg.ReservedInteractiveSlots < 0 {
		cfg.ReservedInteractiveSlots = 0
	}
	if cfg.ReservedInteractiveSlots >= cfg.MaxInFlight {
		cfg.ReservedInteractiveSlots = cfg.MaxInFlight - 1
	}
	if cfg.CircuitFailureThreshold <= 0 {
		cfg.CircuitFailureThreshold = defaults.CircuitFailureThreshold
	}
	if cfg.CircuitOpenDuration <= 0 {
		cfg.CircuitOpenDuration = defaults.CircuitOpenDuration
	}
	if cfg.DefaultRateLimitDelay <= 0 {
		cfg.DefaultRateLimitDelay = defaults.DefaultRateLimitDelay
	}
	if cfg.PriorityAgingInterval <= 0 {
		cfg.PriorityAgingInterval = defaults.PriorityAgingInterval
	}
	if cfg.RecoveryAdmissionInterval <= 0 {
		cfg.RecoveryAdmissionInterval = defaults.RecoveryAdmissionInterval
	}
	return cfg
}

type ProviderCoordinator struct {
	mu     sync.Mutex
	cfg    ProviderCoordinatorConfig
	nextID uint64
	scopes map[ProviderScope]*providerScopeState
}

type providerScopeState struct {
	inFlight           int
	consecutiveFailure int
	cooldownUntil      time.Time
	circuitUntil       time.Time
	circuitGeneration  uint64
	recovering         bool
	recoveryNext       time.Time
	waiters            []*providerWaiter
	timer              *time.Timer
	timerAt            time.Time
}

type providerWaiter struct {
	id       uint64
	priority ProviderPriority
	enqueued time.Time
	ctx      context.Context
	ready    chan *ProviderLease
}

type ProviderLease struct {
	coordinator *ProviderCoordinator
	scope       ProviderScope
	generation  uint64
	once        sync.Once
}

func (l *ProviderLease) Release() {
	l.finish(nil, false)
}

// Succeed records a provider success and releases the admission slot. A
// success from a request admitted before the current circuit generation is
// deliberately ignored so a late response cannot close a newer circuit.
func (l *ProviderLease) Succeed() {
	l.finish(nil, true)
}

// Fail records a provider failure and releases the admission slot.
func (l *ProviderLease) Fail(failure NormalizedFailure) {
	l.finish(&failure, false)
}

func (l *ProviderLease) finish(failure *NormalizedFailure, success bool) {
	if l == nil || l.coordinator == nil {
		return
	}
	l.once.Do(func() {
		l.coordinator.complete(l.scope, l.generation, failure, success)
	})
}

type ProviderCoordinatorSnapshot struct {
	InFlight           int
	Waiting            int
	ConsecutiveFailure int
	CooldownUntil      time.Time
	CircuitUntil       time.Time
	Recovering         bool
	RecoveryNext       time.Time
}

func NewProviderCoordinator(cfg ProviderCoordinatorConfig) *ProviderCoordinator {
	return &ProviderCoordinator{
		cfg:    normalizeProviderCoordinatorConfig(cfg),
		scopes: make(map[ProviderScope]*providerScopeState),
	}
}

func (c *ProviderCoordinator) Acquire(ctx context.Context, scope ProviderScope, priority ProviderPriority) (*ProviderLease, error) {
	if c == nil {
		return &ProviderLease{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	scope = normalizeProviderScope(scope)
	now := time.Now()
	waiter := &providerWaiter{
		ctx:      ctx,
		priority: normalizeProviderPriority(priority),
		enqueued: now,
		ready:    make(chan *ProviderLease, 1),
	}
	c.mu.Lock()
	c.nextID++
	waiter.id = c.nextID
	state := c.stateLocked(scope)
	state.waiters = append(state.waiters, waiter)
	c.scheduleLocked(scope, state, now)
	c.mu.Unlock()

	select {
	case lease := <-waiter.ready:
		if err := ctx.Err(); err != nil {
			lease.Release()
			return nil, err
		}
		return lease, nil
	case <-ctx.Done():
		if !c.cancelWaiter(scope, waiter.id) {
			// scheduleLocked already granted the slot. Drain and release it so a
			// cancellation race cannot leak provider capacity.
			select {
			case lease := <-waiter.ready:
				lease.Release()
			default:
			}
		}
		return nil, ctx.Err()
	}
}

func (c *ProviderCoordinator) observeFailureLocked(state *providerScopeState, failure NormalizedFailure, now time.Time) {
	switch failure.Category {
	case FailureRateLimit, FailureOverloaded:
		delay := failure.RetryAfter
		if delay <= 0 {
			delay = c.cfg.DefaultRateLimitDelay
		}
		if until := now.Add(delay); until.After(state.cooldownUntil) {
			state.cooldownUntil = until
		}
		state.recovering = false
		state.recoveryNext = time.Time{}
	case FailureServer, FailureNetwork, FailureIncompleteStream:
		state.consecutiveFailure++
		if state.consecutiveFailure >= c.cfg.CircuitFailureThreshold {
			if !state.circuitUntil.After(now) {
				state.circuitGeneration++
			}
			if until := now.Add(c.cfg.CircuitOpenDuration); until.After(state.circuitUntil) {
				state.circuitUntil = until
			}
			state.recovering = false
			state.recoveryNext = time.Time{}
		}
	}
}

func (c *ProviderCoordinator) Snapshot(scope ProviderScope) ProviderCoordinatorSnapshot {
	if c == nil {
		return ProviderCoordinatorSnapshot{}
	}
	scope = normalizeProviderScope(scope)
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.stateLocked(scope)
	return ProviderCoordinatorSnapshot{
		InFlight:           state.inFlight,
		Waiting:            len(state.waiters),
		ConsecutiveFailure: state.consecutiveFailure,
		CooldownUntil:      state.cooldownUntil,
		CircuitUntil:       state.circuitUntil,
		Recovering:         state.recovering,
		RecoveryNext:       state.recoveryNext,
	}
}

func (c *ProviderCoordinator) stateLocked(scope ProviderScope) *providerScopeState {
	scope = normalizeProviderScope(scope)
	state := c.scopes[scope]
	if state == nil {
		state = &providerScopeState{}
		c.scopes[scope] = state
	}
	return state
}

func (c *ProviderCoordinator) scheduleLocked(scope ProviderScope, state *providerScopeState, now time.Time) {
	gate := state.cooldownUntil
	if state.circuitUntil.After(gate) {
		gate = state.circuitUntil
	}
	if gate.After(now) {
		c.armTimerLocked(scope, state, gate)
		return
	}
	expiredGate := false
	if !state.cooldownUntil.IsZero() {
		state.cooldownUntil = time.Time{}
		expiredGate = true
	}
	if !state.circuitUntil.IsZero() {
		state.circuitUntil = time.Time{}
		expiredGate = true
	}
	if expiredGate && len(state.waiters) > 0 {
		state.recovering = true
		state.recoveryNext = now
	}
	if state.recovering && state.recoveryNext.After(now) {
		c.armTimerLocked(scope, state, state.recoveryNext)
		return
	}
	if state.timer != nil {
		state.timer.Stop()
		state.timer = nil
		state.timerAt = time.Time{}
	}

	active := state.waiters[:0]
	for _, waiter := range state.waiters {
		if waiter.ctx.Err() == nil {
			active = append(active, waiter)
		}
	}
	state.waiters = active
	sort.SliceStable(state.waiters, func(i, j int) bool {
		left := c.effectivePriority(state.waiters[i], now)
		right := c.effectivePriority(state.waiters[j], now)
		if left != right {
			return left > right
		}
		return state.waiters[i].id < state.waiters[j].id
	})
	admissionLimit := c.cfg.MaxInFlight - state.inFlight
	if state.recovering && admissionLimit > 1 {
		admissionLimit = 1
	}
	admitted := 0
	for state.inFlight < c.cfg.MaxInFlight && admitted < admissionLimit {
		index := c.nextAdmissibleWaiter(state)
		if index < 0 {
			break
		}
		waiter := state.waiters[index]
		state.waiters = append(state.waiters[:index], state.waiters[index+1:]...)
		state.inFlight++
		admitted++
		waiter.ready <- &ProviderLease{
			coordinator: c,
			scope:       scope,
			generation:  state.circuitGeneration,
		}
	}
	if state.recovering {
		if admitted > 0 && c.nextAdmissibleWaiter(state) >= 0 {
			state.recoveryNext = now.Add(c.cfg.RecoveryAdmissionInterval)
			c.armTimerLocked(scope, state, state.recoveryNext)
		} else {
			state.recovering = false
			state.recoveryNext = time.Time{}
		}
	}
}

func (c *ProviderCoordinator) effectivePriority(waiter *providerWaiter, now time.Time) ProviderPriority {
	priority := waiter.priority
	if waited := now.Sub(waiter.enqueued); waited > 0 {
		priority += ProviderPriority(waited/c.cfg.PriorityAgingInterval) * 10
	}
	if priority > ProviderPriorityCritical {
		return ProviderPriorityCritical
	}
	return priority
}

func (c *ProviderCoordinator) nextAdmissibleWaiter(state *providerScopeState) int {
	for index, waiter := range state.waiters {
		if waiter.priority >= ProviderPriorityInteractive {
			return index
		}
		lowPriorityCapacity := c.cfg.MaxInFlight - c.cfg.ReservedInteractiveSlots
		if state.inFlight < lowPriorityCapacity {
			return index
		}
	}
	return -1
}

func (c *ProviderCoordinator) armTimerLocked(scope ProviderScope, state *providerScopeState, when time.Time) {
	if state.timer != nil && !when.Before(state.timerAt) {
		return
	}
	if state.timer != nil {
		state.timer.Stop()
	}
	state.timerAt = when
	delay := time.Until(when)
	if delay < 0 {
		delay = 0
	}
	state.timer = time.AfterFunc(delay, func() {
		c.mu.Lock()
		current := c.scopes[scope]
		if current == state {
			state.timer = nil
			state.timerAt = time.Time{}
			c.scheduleLocked(scope, state, time.Now())
		}
		c.mu.Unlock()
	})
}

func (c *ProviderCoordinator) cancelWaiter(scope ProviderScope, id uint64) bool {
	scope = normalizeProviderScope(scope)
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.stateLocked(scope)
	for i, waiter := range state.waiters {
		if waiter.id != id {
			continue
		}
		state.waiters = append(state.waiters[:i], state.waiters[i+1:]...)
		c.scheduleLocked(scope, state, time.Now())
		return true
	}
	return false
}

func (c *ProviderCoordinator) complete(scope ProviderScope, generation uint64, failure *NormalizedFailure, success bool) {
	scope = normalizeProviderScope(scope)
	now := time.Now()
	c.mu.Lock()
	state := c.stateLocked(scope)
	if failure != nil {
		c.observeFailureLocked(state, *failure, now)
	} else if success && generation == state.circuitGeneration && !state.circuitUntil.After(now) {
		state.consecutiveFailure = 0
		state.circuitUntil = time.Time{}
	}
	if state.inFlight > 0 {
		state.inFlight--
	}
	c.scheduleLocked(scope, state, now)
	c.mu.Unlock()
}

func normalizeProviderPriority(priority ProviderPriority) ProviderPriority {
	switch priority {
	case ProviderPriorityBestEffort, ProviderPriorityBackground, ProviderPriorityInteractive, ProviderPriorityCritical:
		return priority
	default:
		return ProviderPriorityInteractive
	}
}

func normalizeProviderScope(scope ProviderScope) ProviderScope {
	if strings.TrimSpace(string(scope)) == "" {
		return ProviderScope("unknown-provider#default")
	}
	return scope
}
