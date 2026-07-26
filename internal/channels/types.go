package channels

import "time"

const (
	MaxRoomMembers  = 32
	MaxMessageRunes = 4000
	DraftExpiry     = 24 * time.Hour
	MinReminderDur  = time.Minute
	ThreadStreakCap = 6
)

type RoomKind string

const (
	RoomChannel RoomKind = "channel"
	RoomDM      RoomKind = "dm"
)

type MemberType string

const (
	MemberHuman MemberType = "human"
	MemberAgent MemberType = "agent"
)

type MessageKind string

const (
	MessageText   MessageKind = "text"
	MessageTask   MessageKind = "task"
	MessageSystem MessageKind = "system"
)

type TaskState string

const (
	TaskStateOpen  TaskState = "open"
	TaskStateDoing TaskState = "doing"
	TaskStateDone  TaskState = "done"
)

type InboxKind string

const (
	InboxMention      InboxKind = "mention"
	InboxReply        InboxKind = "reply"
	InboxThreadUpdate InboxKind = "thread_update"
	InboxTask         InboxKind = "task"
	InboxReminder     InboxKind = "reminder"
)

type ReminderState string

const (
	ReminderPending   ReminderState = "pending"
	ReminderFired     ReminderState = "fired"
	ReminderCancelled ReminderState = "cancelled"
)

type NamedAgent struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	MemoryDir        string    `json:"memory_dir"`
	AvatarKey        string    `json:"avatar_key"`
	AvatarImage      string    `json:"avatar_image,omitempty"`
	ProviderOverride string    `json:"provider_override,omitempty"`
	ModelOverride    string    `json:"model_override,omitempty"`
	Autostart        bool      `json:"autostart"`
	CreatedAt        time.Time `json:"created_at"`
	ActivityStatus   string    `json:"activity_status,omitempty"`
}

type AgentCredential struct {
	Agent NamedAgent `json:"agent"`
	Token string     `json:"token"`
}

type CreateNamedAgentParams struct {
	Name             string
	AvatarKey        string
	AvatarImage      string
	ProviderOverride string
	ModelOverride    string
	Autostart        bool
}

type UpdateNamedAgentParams struct {
	ID               string
	Name             string
	AvatarKey        string
	AvatarImage      *string
	ProviderOverride string
	ModelOverride    string
}

type BootstrapResult struct {
	Agents []NamedAgent `json:"agents"`
	Rooms  []Room       `json:"rooms"`
}

type RoomMember struct {
	RoomID     string     `json:"room_id"`
	MemberType MemberType `json:"member_type"`
	MemberID   string     `json:"member_id"`
	JoinedAt   time.Time  `json:"joined_at"`
}

type Room struct {
	ID        string       `json:"id"`
	Kind      RoomKind     `json:"kind"`
	Name      string       `json:"name"`
	CreatedBy string       `json:"created_by"`
	CreatedAt time.Time    `json:"created_at"`
	Members   []RoomMember `json:"members"`
}

type CreateRoomParams struct {
	Kind      RoomKind
	Name      string
	CreatedBy string
	Members   []RoomMember
}

type UpdateRoomParams struct {
	RoomID string
	Name   string
}

type Message struct {
	ID         string         `json:"id"`
	RoomID     string         `json:"room_id"`
	Seq        int64          `json:"seq"`
	ThreadID   string         `json:"thread_id,omitempty"`
	AuthorType MemberType     `json:"author_type"`
	AuthorID   string         `json:"author_id"`
	Kind       MessageKind    `json:"kind"`
	Body       string         `json:"body"`
	Images     []MessageImage `json:"images,omitempty"`
	Files      []MessageFile  `json:"files,omitempty"`
	Mentions   []string       `json:"mentions"`
	ReplyTo    string         `json:"reply_to,omitempty"`
	TaskTitle  string         `json:"task_title,omitempty"`
	TaskState  string         `json:"task_state,omitempty"`
	TaskOwner  string         `json:"task_owner,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

type MessageImage struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
	Width     uint32 `json:"width,omitempty"`
	Height    uint32 `json:"height,omitempty"`
}

type MessageFile struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
	Filename  string `json:"filename,omitempty"`
}

type HumanSendParams struct {
	RoomID   string
	HumanID  string
	ThreadID string
	ReplyTo  string
	Body     string
	Images   []MessageImage
	Files    []MessageFile
}

type AgentSendParams struct {
	RoomID   string
	AgentID  string
	Token    string
	ThreadID string
	ReplyTo  string
	Body     string
	BasisSeq int64
}

type SendResult struct {
	Status       SendStatus  `json:"status"`
	Message      Message     `json:"message"`
	Draft        *Draft      `json:"draft,omitempty"`
	Delta        *DraftDelta `json:"delta,omitempty"`
	WakeAgentIDs []string    `json:"wake_agent_ids,omitempty"`
}

type SendStatus string

const (
	SendCommitted SendStatus = "committed"
	SendHeld      SendStatus = "held"
)

type DraftState string

const (
	DraftHeld      DraftState = "held"
	DraftDropped   DraftState = "dropped"
	DraftCommitted DraftState = "committed"
	DraftExpired   DraftState = "expired"
)

type DraftResolution string

const (
	DraftAsIs   DraftResolution = "as_is"
	DraftSilent DraftResolution = "silent"
	DraftAnyway DraftResolution = "anyway"
)

type Draft struct {
	ID        string     `json:"id"`
	AgentID   string     `json:"agent_id"`
	RoomID    string     `json:"room_id"`
	ThreadID  string     `json:"thread_id,omitempty"`
	Body      string     `json:"body"`
	BasisSeq  int64      `json:"basis_seq"`
	HoldCount int        `json:"hold_count"`
	State     DraftState `json:"state"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type DraftDeltaItem struct {
	MessageID  string     `json:"message_id"`
	Seq        int64      `json:"seq"`
	AuthorType MemberType `json:"author_type"`
	AuthorID   string     `json:"author_id"`
	Preview    string     `json:"preview"`
}

type DraftDelta struct {
	Count int              `json:"count"`
	Items []DraftDeltaItem `json:"items"`
}

type ResolveDraftParams struct {
	AgentID    string
	Token      string
	DraftID    string
	Resolution DraftResolution
	BasisSeq   *int64
}

type DraftResult struct {
	Status  SendStatus  `json:"status,omitempty"`
	Draft   Draft       `json:"draft"`
	Message *Message    `json:"message,omitempty"`
	Delta   *DraftDelta `json:"delta,omitempty"`
}

type InboxItem struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	RoomID    string    `json:"room_id"`
	MessageID string    `json:"message_id"`
	Kind      InboxKind `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
	PulledAt  time.Time `json:"pulled_at,omitempty"`
}

type CheckItem struct {
	ID         string     `json:"id"`
	RoomID     string     `json:"room_id"`
	MessageID  string     `json:"message_id"`
	ThreadID   string     `json:"thread_id,omitempty"`
	AuthorType MemberType `json:"author_type"`
	AuthorID   string     `json:"author_id"`
	Kind       InboxKind  `json:"kind"`
	Preview    string     `json:"preview"`
	Seq        int64      `json:"seq"`
	CreatedAt  time.Time  `json:"created_at"`
}

type ScopeSequence struct {
	RoomID   string `json:"room_id"`
	ThreadID string `json:"thread_id,omitempty"`
	Seq      int64  `json:"seq"`
}

type CheckResult struct {
	Items     []CheckItem     `json:"items"`
	Reminders []Reminder      `json:"reminders"`
	Scopes    []ScopeSequence `json:"scopes"`
	HasMore   bool            `json:"has_more"`
	CheckedAt time.Time       `json:"checked_at"`
}

type WakeState struct {
	AgentID     string    `json:"agent_id"`
	Outstanding bool      `json:"outstanding"`
	Pending     bool      `json:"pending"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type WakeSink interface {
	Deliver(agentID string)
}

type TelemetryEvent struct {
	Name       string     `json:"name"`
	MemberType MemberType `json:"member_type,omitempty"`
	MemberID   string     `json:"member_id,omitempty"`
	RoomID     string     `json:"room_id,omitempty"`
	ThreadID   string     `json:"thread_id,omitempty"`
	WakeCount  int        `json:"wake_count,omitempty"`
	HoldCount  int        `json:"hold_count,omitempty"`
}

type TelemetrySink interface {
	RecordChannelEvent(event TelemetryEvent)
}

type Reminder struct {
	ID        string        `json:"id"`
	AgentID   string        `json:"agent_id"`
	FireAt    time.Time     `json:"fire_at"`
	Note      string        `json:"note"`
	RoomID    string        `json:"room_id,omitempty"`
	ThreadID  string        `json:"thread_id,omitempty"`
	State     ReminderState `json:"state"`
	CreatedAt time.Time     `json:"created_at"`
}

type TaskCreateParams struct {
	RoomID   string
	ThreadID string
	Title    string
	Body     string
	OwnerID  string
	AgentID  string
	Token    string
	HumanID  string
}

type TaskUpdateParams struct {
	TaskID  string
	RoomID  string
	State   TaskState
	OwnerID string
	AgentID string
	Token   string
	HumanID string
}

type TaskListParams struct {
	RoomID  string
	OwnerID string
	AgentID string
	Token   string
	HumanID string
}

type ReminderSetParams struct {
	AgentID  string
	Token    string
	FireAt   time.Time
	Note     string
	RoomID   string
	ThreadID string
}

type ReminderListParams struct {
	AgentID string
	Token   string
	State   ReminderState
}

type ReminderCancelParams struct {
	AgentID    string
	Token      string
	ReminderID string
}

type HumanMentionItem struct {
	ID         string     `json:"id"`
	HumanID    string     `json:"human_id"`
	RoomID     string     `json:"room_id"`
	MessageID  string     `json:"message_id"`
	AuthorID   string     `json:"author_id"`
	AuthorType MemberType `json:"author_type"`
	Preview    string     `json:"preview"`
	CreatedAt  time.Time  `json:"created_at"`
}

type HumanMentionCount struct {
	RoomID      string `json:"room_id"`
	UnreadCount int    `json:"unread_count"`
}
