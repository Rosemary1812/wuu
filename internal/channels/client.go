package channels

import (
	"context"
	"errors"
	"strings"
	"time"
)

type AgentClient struct {
	service *Service
	agentID string
	token   string
}

func (s *Service) BindAgent(ctx context.Context, agentID string) (*AgentClient, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, errors.New("named agent id is required")
	}
	token, err := s.loadAgentToken(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return &AgentClient{service: s, agentID: agentID, token: token}, nil
}

func (c *AgentClient) AgentID() string {
	if c == nil {
		return ""
	}
	return c.agentID
}

func (c *AgentClient) Check(ctx context.Context) (CheckResult, error) {
	if c == nil || c.service == nil {
		return CheckResult{}, errors.New("chat agent is not bound")
	}
	return c.service.Check(ctx, c.agentID, c.token)
}

func (c *AgentClient) ReadInbox(ctx context.Context, itemIDs []string) ([]Message, error) {
	if c == nil || c.service == nil {
		return nil, errors.New("chat agent is not bound")
	}
	return c.service.ReadInboxMessages(ctx, c.agentID, c.token, itemIDs)
}

func (c *AgentClient) ReadRoom(ctx context.Context, roomID string, afterSeq int64, limit int) ([]Message, error) {
	if c == nil || c.service == nil {
		return nil, errors.New("chat agent is not bound")
	}
	return c.service.ReadAgentMessages(ctx, c.agentID, c.token, roomID, afterSeq, limit)
}

func (c *AgentClient) Send(ctx context.Context, params AgentSendParams) (SendResult, error) {
	if c == nil || c.service == nil {
		return SendResult{}, errors.New("chat agent is not bound")
	}
	if params.BasisSeq < 0 {
		return SendResult{}, errors.New("message basis sequence cannot be negative")
	}
	params.AgentID = c.agentID
	params.Token = c.token
	return c.service.SendAgent(ctx, params)
}

func (c *AgentClient) ListDrafts(ctx context.Context) ([]Draft, error) {
	if c == nil || c.service == nil {
		return nil, errors.New("chat agent is not bound")
	}
	return c.service.ListDrafts(ctx, c.agentID, c.token)
}

func (c *AgentClient) CreateTask(ctx context.Context, params TaskCreateParams) (Message, error) {
	if c == nil || c.service == nil {
		return Message{}, errors.New("chat agent is not bound")
	}
	params.AgentID = c.agentID
	params.Token = c.token
	return c.service.CreateTask(ctx, params)
}

func (c *AgentClient) UpdateTask(ctx context.Context, params TaskUpdateParams) (Message, error) {
	if c == nil || c.service == nil {
		return Message{}, errors.New("chat agent is not bound")
	}
	params.AgentID = c.agentID
	params.Token = c.token
	return c.service.UpdateTask(ctx, params)
}

func (c *AgentClient) ListTasks(ctx context.Context, roomID string) ([]Message, error) {
	if c == nil || c.service == nil {
		return nil, errors.New("chat agent is not bound")
	}
	return c.service.ListTasks(ctx, TaskListParams{RoomID: roomID, AgentID: c.agentID, Token: c.token})
}

func (c *AgentClient) SetReminder(ctx context.Context, params ReminderSetParams) (Reminder, error) {
	if c == nil || c.service == nil {
		return Reminder{}, errors.New("chat agent is not bound")
	}
	params.AgentID = c.agentID
	params.Token = c.token
	return c.service.SetReminder(ctx, params)
}

func (c *AgentClient) SetReminderAfter(ctx context.Context, delay time.Duration, params ReminderSetParams) (Reminder, error) {
	if c == nil || c.service == nil {
		return Reminder{}, errors.New("chat agent is not bound")
	}
	params.AgentID = c.agentID
	params.Token = c.token
	return c.service.SetReminderAfter(ctx, params, delay)
}

func (c *AgentClient) ListReminders(ctx context.Context, state ReminderState) ([]Reminder, error) {
	if c == nil || c.service == nil {
		return nil, errors.New("chat agent is not bound")
	}
	return c.service.ListReminders(ctx, ReminderListParams{AgentID: c.agentID, Token: c.token, State: state})
}

func (c *AgentClient) CancelReminder(ctx context.Context, reminderID string) (Reminder, error) {
	if c == nil || c.service == nil {
		return Reminder{}, errors.New("chat agent is not bound")
	}
	return c.service.CancelReminder(ctx, ReminderCancelParams{AgentID: c.agentID, Token: c.token, ReminderID: reminderID})
}

func (c *AgentClient) ResolveDraft(ctx context.Context, params ResolveDraftParams) (DraftResult, error) {
	if c == nil || c.service == nil {
		return DraftResult{}, errors.New("chat agent is not bound")
	}
	params.AgentID = c.agentID
	params.Token = c.token
	return c.service.ResolveDraft(ctx, params)
}
