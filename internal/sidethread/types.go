package sidethread

import "time"

// Status is the lifecycle state of a side thread. The set matches
// SideThreadStatus in packages/protocol/src/index.ts.
type Status string

const (
	StatusIdle        Status = "idle"
	StatusRunning     Status = "running"
	StatusCompleted   Status = "completed"
	StatusFailed      Status = "failed"
	StatusInterrupted Status = "interrupted"
	// StatusDetached marks a side thread whose main thread was deleted.
	// The on-disk record is preserved so the renderer can briefly show
	// the "inaccessible" state, but no further turns run.
	StatusDetached Status = "detached"
)

type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
)

// AssistantMessageStatus mirrors SideThreadMessage.status in the
// protocol. It is only meaningful on assistant messages.
type AssistantMessageStatus string

const (
	AssistantStreaming   AssistantMessageStatus = "streaming"
	AssistantCompleted   AssistantMessageStatus = "completed"
	AssistantFailed      AssistantMessageStatus = "failed"
	AssistantInterrupted AssistantMessageStatus = "interrupted"
)

// Message is one entry in the side thread's independent history. Side
// threads own their own messages and never append to the main thread.
type Message struct {
	ID           string                 `json:"id"`
	SideThreadID string                 `json:"side_thread_id"`
	Role         MessageRole            `json:"role"`
	Text         string                 `json:"text"`
	Status       AssistantMessageStatus `json:"status,omitempty"`
	ErrorText    string                 `json:"error_message,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
}

// SideThread is the on-disk representation of one side thread. Files
// are keyed by MainThreadID; at most one SideThread exists per main
// thread.
type SideThread struct {
	// SideThreadID is the thread id the app-server turn pipeline uses
	// for any agent runs this side thread triggers. It is generated
	// lazily on the first message and persisted.
	SideThreadID string    `json:"side_thread_id"`
	MainThreadID string    `json:"main_thread_id"`
	Status       Status    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Messages     []Message `json:"messages"`
}

// Summary is the lightweight view of a side thread for IPC.
type Summary struct {
	SideThreadID string    `json:"side_thread_id"`
	MainThreadID string    `json:"main_thread_id"`
	Status       Status    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Summary returns the lightweight view of st.
func (st *SideThread) Summary() Summary {
	if st == nil {
		return Summary{}
	}
	return Summary{
		SideThreadID: st.SideThreadID,
		MainThreadID: st.MainThreadID,
		Status:       st.Status,
		CreatedAt:    st.CreatedAt,
		UpdatedAt:    st.UpdatedAt,
	}
}
