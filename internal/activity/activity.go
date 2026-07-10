package activity

import "time"

type Kind string

const (
	KindBrowser Kind = "browser"
	KindCUA     Kind = "cua"
)

type State string

const (
	StateStarting            State = "starting"
	StateActive              State = "active"
	StateUserControlled      State = "user_controlled"
	StateWaitingConfirmation State = "waiting_confirmation"
	StateStopped             State = "stopped"
	StateError               State = "error"
)

type Controller string

const (
	ControllerAgent Controller = "agent"
	ControllerUser  Controller = "user"
	ControllerNone  Controller = "none"
)

type EventType string

const (
	EventStarted        EventType = "started"
	EventUpdated        EventType = "updated"
	EventControlChanged EventType = "control_changed"
	EventStopped        EventType = "stopped"
)

type Event struct {
	Type     EventType
	Activity Session
}

type Session struct {
	ID         string     `json:"id"`
	Kind       Kind       `json:"kind"`
	ThreadID   string     `json:"thread_id"`
	Workdir    string     `json:"workdir"`
	PluginID   string     `json:"plugin_id,omitempty"`
	Target     string     `json:"target,omitempty"`
	State      State      `json:"state"`
	Controller Controller `json:"controller"`
	Preview    string     `json:"preview,omitempty"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type Lease struct {
	ActivityID string `json:"activity_id"`
	ThreadID   string `json:"thread_id"`
	Token      string `json:"token"`
}

type StartOptions struct {
	ID       string
	Kind     Kind
	ThreadID string
	Workdir  string
	PluginID string
	Target   string
	Preview  string
}

type UpdateOptions struct {
	State   State
	Target  string
	Preview string
	Error   string
}
