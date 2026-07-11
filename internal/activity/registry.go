package activity

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound       = errors.New("activity not found")
	ErrAlreadyExists  = errors.New("activity already exists")
	ErrThreadMismatch = errors.New("activity belongs to another thread")
	ErrControlRevoked = errors.New("activity control revoked")
	ErrStopped        = errors.New("activity is stopped")
)

type registryEntry struct {
	session    Session
	leaseToken string
}

type Registry struct {
	mu             sync.RWMutex
	entries        map[string]*registryEntry
	listeners      map[uint64]func(Event)
	nextListenerID uint64
	now            func() time.Time
}

func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]*registryEntry), listeners: make(map[uint64]func(Event)), now: time.Now}
}

func (r *Registry) Subscribe(listener func(Event)) func() {
	if r == nil || listener == nil {
		return func() {}
	}
	r.mu.Lock()
	r.nextListenerID++
	id := r.nextListenerID
	r.listeners[id] = listener
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.listeners, id)
		r.mu.Unlock()
	}
}

func (r *Registry) Start(options StartOptions) (Session, Lease, error) {
	if r == nil {
		return Session{}, Lease{}, errors.New("activity registry is unavailable")
	}
	options.ThreadID = strings.TrimSpace(options.ThreadID)
	options.Workdir = strings.TrimSpace(options.Workdir)
	if options.ThreadID == "" || options.Workdir == "" {
		return Session{}, Lease{}, errors.New("activity thread_id and workdir are required")
	}
	if !validKind(options.Kind) {
		return Session{}, Lease{}, fmt.Errorf("unsupported activity kind %q", options.Kind)
	}
	id := strings.TrimSpace(options.ID)
	if id == "" {
		var err error
		id, err = randomID("activity", 12)
		if err != nil {
			return Session{}, Lease{}, err
		}
	}
	token, err := randomID("lease", 24)
	if err != nil {
		return Session{}, Lease{}, err
	}
	now := r.now().UTC()
	session := Session{
		ID:         id,
		Kind:       options.Kind,
		ThreadID:   options.ThreadID,
		Workdir:    options.Workdir,
		PluginID:   strings.TrimSpace(options.PluginID),
		Target:     strings.TrimSpace(options.Target),
		State:      StateStarting,
		Controller: ControllerAgent,
		Preview:    strings.TrimSpace(options.Preview),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	r.mu.Lock()
	if _, exists := r.entries[id]; exists {
		r.mu.Unlock()
		return Session{}, Lease{}, ErrAlreadyExists
	}
	r.entries[id] = &registryEntry{session: session, leaseToken: token}
	r.mu.Unlock()
	r.emit(Event{Type: EventStarted, Activity: session})
	return session, Lease{ActivityID: id, ThreadID: options.ThreadID, Token: token}, nil
}

// Acquire returns the current agent lease for one plugin Activity or creates
// the Activity on first use. A user takeover and a stopped tombstone are hard
// gates: callers cannot silently create a replacement session and continue
// controlling the UI behind the user's back.
func (r *Registry) Acquire(options StartOptions) (Session, Lease, error) {
	if r == nil {
		return Session{}, Lease{}, errors.New("activity registry is unavailable")
	}
	options.ThreadID = strings.TrimSpace(options.ThreadID)
	options.Workdir = strings.TrimSpace(options.Workdir)
	options.PluginID = strings.TrimSpace(options.PluginID)
	if options.ThreadID == "" || options.Workdir == "" || options.PluginID == "" {
		return Session{}, Lease{}, errors.New("activity thread_id, workdir, and plugin_id are required")
	}
	if !validKind(options.Kind) {
		return Session{}, Lease{}, fmt.Errorf("unsupported activity kind %q", options.Kind)
	}

	r.mu.Lock()
	var current *registryEntry
	for _, entry := range r.entries {
		if entry.session.ThreadID != options.ThreadID || entry.session.PluginID != options.PluginID || entry.session.Kind != options.Kind {
			continue
		}
		if current == nil || entry.session.CreatedAt.After(current.session.CreatedAt) {
			current = entry
		}
	}
	if current != nil {
		session := current.session
		switch {
		case session.State == StateStopped:
			r.mu.Unlock()
			return Session{}, Lease{}, ErrStopped
		case session.Controller != ControllerAgent || current.leaseToken == "":
			r.mu.Unlock()
			return Session{}, Lease{}, ErrControlRevoked
		default:
			lease := Lease{ActivityID: session.ID, ThreadID: session.ThreadID, Token: current.leaseToken}
			r.mu.Unlock()
			return session, lease, nil
		}
	}

	id := strings.TrimSpace(options.ID)
	if id == "" {
		var err error
		id, err = randomID("activity", 12)
		if err != nil {
			r.mu.Unlock()
			return Session{}, Lease{}, err
		}
	}
	if _, exists := r.entries[id]; exists {
		r.mu.Unlock()
		return Session{}, Lease{}, ErrAlreadyExists
	}
	token, err := randomID("lease", 24)
	if err != nil {
		r.mu.Unlock()
		return Session{}, Lease{}, err
	}
	now := r.now().UTC()
	session := Session{
		ID:         id,
		Kind:       options.Kind,
		ThreadID:   options.ThreadID,
		Workdir:    options.Workdir,
		PluginID:   options.PluginID,
		Target:     strings.TrimSpace(options.Target),
		State:      StateStarting,
		Controller: ControllerAgent,
		Preview:    strings.TrimSpace(options.Preview),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	r.entries[id] = &registryEntry{session: session, leaseToken: token}
	r.mu.Unlock()
	r.emit(Event{Type: EventStarted, Activity: session})
	return session, Lease{ActivityID: id, ThreadID: options.ThreadID, Token: token}, nil
}

func (r *Registry) List(threadID string) []Session {
	if r == nil {
		return nil
	}
	threadID = strings.TrimSpace(threadID)
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Session, 0)
	for _, entry := range r.entries {
		if entry.session.ThreadID == threadID {
			out = append(out, entry.session)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (r *Registry) Update(threadID, activityID string, options UpdateOptions) (Session, error) {
	r.mu.Lock()
	entry, err := r.entryLocked(threadID, activityID)
	if err != nil {
		r.mu.Unlock()
		return Session{}, err
	}
	if entry.session.State == StateStopped {
		r.mu.Unlock()
		return Session{}, ErrStopped
	}
	if options.State != "" {
		if !validState(options.State) {
			r.mu.Unlock()
			return Session{}, fmt.Errorf("unsupported activity state %q", options.State)
		}
		entry.session.State = options.State
		if options.State == StateStopped {
			entry.leaseToken = ""
			entry.session.Controller = ControllerNone
		}
	}
	if options.Target != "" {
		entry.session.Target = strings.TrimSpace(options.Target)
	}
	if options.Preview != "" {
		entry.session.Preview = strings.TrimSpace(options.Preview)
	}
	if options.ClearError {
		entry.session.Error = ""
	} else if options.Error != "" {
		entry.session.Error = strings.TrimSpace(options.Error)
	}
	if options.Interaction != nil {
		interaction := *options.Interaction
		entry.session.Interaction = &interaction
	}
	entry.session.UpdatedAt = r.now().UTC()
	session := entry.session
	r.mu.Unlock()
	eventType := EventUpdated
	if session.State == StateStopped {
		eventType = EventStopped
	}
	r.emit(Event{Type: eventType, Activity: session})
	return session, nil
}

func (r *Registry) Takeover(threadID, activityID string) (Session, error) {
	r.mu.Lock()
	entry, err := r.entryLocked(threadID, activityID)
	if err != nil {
		r.mu.Unlock()
		return Session{}, err
	}
	if entry.session.State == StateStopped {
		r.mu.Unlock()
		return Session{}, ErrStopped
	}
	entry.leaseToken = ""
	entry.session.Controller = ControllerUser
	entry.session.State = StateUserControlled
	entry.session.UpdatedAt = r.now().UTC()
	session := entry.session
	r.mu.Unlock()
	r.emit(Event{Type: EventControlChanged, Activity: session})
	return session, nil
}

func (r *Registry) Release(threadID, activityID string) (Session, Lease, error) {
	r.mu.Lock()
	entry, err := r.entryLocked(threadID, activityID)
	if err != nil {
		r.mu.Unlock()
		return Session{}, Lease{}, err
	}
	if entry.session.State == StateStopped {
		r.mu.Unlock()
		return Session{}, Lease{}, ErrStopped
	}
	token, err := randomID("lease", 24)
	if err != nil {
		r.mu.Unlock()
		return Session{}, Lease{}, err
	}
	entry.leaseToken = token
	entry.session.Controller = ControllerAgent
	entry.session.State = StateBackgroundControlled
	entry.session.UpdatedAt = r.now().UTC()
	session := entry.session
	r.mu.Unlock()
	r.emit(Event{Type: EventControlChanged, Activity: session})
	return session, Lease{ActivityID: session.ID, ThreadID: session.ThreadID, Token: token}, nil
}

func (r *Registry) Stop(threadID, activityID string) (Session, error) {
	r.mu.Lock()
	entry, err := r.entryLocked(threadID, activityID)
	if err != nil {
		r.mu.Unlock()
		return Session{}, err
	}
	if entry.session.State == StateStopped {
		session := entry.session
		r.mu.Unlock()
		return session, nil
	}
	entry.leaseToken = ""
	entry.session.State = StateStopped
	entry.session.Controller = ControllerNone
	entry.session.UpdatedAt = r.now().UTC()
	session := entry.session
	r.mu.Unlock()
	r.emit(Event{Type: EventStopped, Activity: session})
	return session, nil
}

func (r *Registry) CheckControl(threadID, activityID, token string) error {
	if r == nil {
		return ErrControlRevoked
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, err := r.entryLocked(threadID, activityID)
	if err != nil {
		return err
	}
	if entry.session.Controller != ControllerAgent || entry.session.State == StateStopped || entry.leaseToken == "" || entry.leaseToken != strings.TrimSpace(token) {
		return ErrControlRevoked
	}
	return nil
}

func (r *Registry) entryLocked(threadID, activityID string) (*registryEntry, error) {
	entry, ok := r.entries[strings.TrimSpace(activityID)]
	if !ok {
		return nil, ErrNotFound
	}
	if entry.session.ThreadID != strings.TrimSpace(threadID) {
		return nil, ErrThreadMismatch
	}
	return entry, nil
}

func (r *Registry) emit(event Event) {
	if r == nil {
		return
	}
	r.mu.RLock()
	listeners := make([]func(Event), 0, len(r.listeners))
	for _, listener := range r.listeners {
		listeners = append(listeners, listener)
	}
	r.mu.RUnlock()
	for _, listener := range listeners {
		listener(event)
	}
}

func validKind(kind Kind) bool {
	return kind == KindBrowser || kind == KindCUA
}

func validState(state State) bool {
	switch state {
	case StateStarting, StateActive, StateBackgroundControlled, StateForegroundControlled, StateUserControlled, StateWaitingConfirmation, StateStopped, StateError:
		return true
	default:
		return false
	}
}

func randomID(prefix string, size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return prefix + "-" + base64.RawURLEncoding.EncodeToString(data), nil
}
