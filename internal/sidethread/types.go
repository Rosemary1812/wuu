package sidethread

import "time"

// Status is the lifecycle state of a side thread. The set matches
// SideThreadStatus in packages/protocol/src/index.ts.
type Status string

const (
	StatusRunning     Status = "running"
	StatusCompleted   Status = "completed"
	StatusFailed      Status = "failed"
	StatusInterrupted Status = "interrupted"
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
	Revision     uint64    `json:"revision"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Messages     []Message `json:"messages"`
}
