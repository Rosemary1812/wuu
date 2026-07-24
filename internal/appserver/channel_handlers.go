package appserver

import (
	"context"
	"errors"
	"strings"

	"github.com/blueberrycongee/wuu/internal/channels"
	"github.com/blueberrycongee/wuu/internal/session"
)

const localChannelHumanID = "local-user"

func (s *Server) handleChannelBootstrap(ctx context.Context, req Request) error {
	if s == nil || s.channelService == nil {
		return s.writeResponse(req.ID, nil, errors.New("channels service is unavailable"))
	}
	result, err := s.channelService.EnsureBootstrap(ctx, localChannelHumanID)
	return s.writeResponse(req.ID, ChannelBootstrapResult(result), err)
}

func (s *Server) handleChannelAgentList(ctx context.Context, req Request) error {
	if s == nil || s.channelService == nil {
		return s.writeResponse(req.ID, nil, errors.New("channels service is unavailable"))
	}
	agents, err := s.channelService.ListNamedAgents(ctx)
	if err == nil {
		for i := range agents {
			agents[i].ActivityStatus = "idle"
			if thread := s.thread(namedAgentSessionID(agents[i])); thread != nil && threadIsRunning(thread) {
				agents[i].ActivityStatus = "thinking"
			}
		}
	}
	return s.writeResponse(req.ID, ChannelAgentListResult{Agents: agents}, err)
}

func (s *Server) handleChannelAgentCreate(ctx context.Context, req Request) error {
	if s == nil || s.channelService == nil {
		return s.writeResponse(req.ID, nil, errors.New("channels service is unavailable"))
	}
	var params ChannelAgentCreateParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	credential, err := s.channelService.CreateNamedAgent(ctx, channels.CreateNamedAgentParams{
		Name: params.Name, AvatarKey: params.AvatarKey, AvatarImage: params.AvatarImage, ProviderOverride: params.ProviderOverride, ModelOverride: params.ModelOverride, Autostart: true,
	})
	return s.writeResponse(req.ID, ChannelAgentCreateResult{Agent: credential.Agent}, err)
}

func (s *Server) handleChannelAgentUpdate(ctx context.Context, req Request) error {
	if s == nil || s.channelService == nil {
		return s.writeResponse(req.ID, nil, errors.New("channels service is unavailable"))
	}
	var params ChannelAgentUpdateParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	current, err := s.channelService.GetNamedAgent(ctx, params.AgentID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	thread := s.thread(namedAgentSessionID(current))
	if thread != nil && threadIsRunning(thread) {
		return s.writeResponse(req.ID, nil, errors.New("named agent cannot be edited while it is running"))
	}
	agent, err := s.channelService.UpdateNamedAgent(ctx, channels.UpdateNamedAgentParams{
		ID: params.AgentID, Name: params.Name, AvatarKey: params.AvatarKey, AvatarImage: params.AvatarImage, ProviderOverride: params.ProviderOverride, ModelOverride: params.ModelOverride,
	})
	if err == nil && thread != nil {
		selection := s.currentSessionRuntimeSelection()
		if agent.ModelOverride != "" {
			selection.Provider = agent.ProviderOverride
			selection.Model = agent.ModelOverride
		}
		thread.mu.Lock()
		oldRuntime := thread.execRuntime
		thread.execRuntime = nil
		thread.Title = agent.Name
		applyThreadRuntimeSelection(thread, selection)
		thread.mu.Unlock()
		if oldRuntime != nil {
			releaseDetachedThreadRuntime(detachedThreadRuntime{runtime: oldRuntime})
		}
		if s.rt != nil {
			_, _ = session.UpdateTitle(s.rt.SessionDir, thread.ID, agent.Name)
			_, _ = session.SetRuntimeSelection(s.rt.SessionDir, thread.ID, selection)
		}
	}
	return s.writeResponse(req.ID, ChannelAgentUpdateResult{Agent: agent}, err)
}

func (s *Server) handleChannelAgentDelete(ctx context.Context, req Request) error {
	if s == nil || s.channelService == nil {
		return s.writeResponse(req.ID, nil, errors.New("channels service is unavailable"))
	}
	var params ChannelAgentDeleteParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	err := s.channelService.DeleteNamedAgent(ctx, params.AgentID)
	return s.writeResponse(req.ID, ChannelAgentDeleteResult{Deleted: err == nil}, err)
}

func (s *Server) handleChannelAgentStart(ctx context.Context, req Request) error {
	if s == nil || s.channelService == nil {
		return s.writeResponse(req.ID, nil, errors.New("channels service is unavailable"))
	}
	var params ChannelAgentStartParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	agent, err := s.channelService.GetNamedAgent(ctx, params.AgentID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	s.namedAgentMu.Lock()
	defer s.namedAgentMu.Unlock()
	thread, err := s.ensureNamedAgentThreadLocked(agent)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	state, err := s.channelService.WakeState(ctx, agent.ID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if state.Outstanding && !threadIsRunning(thread) {
		if err := s.startNamedAgentWakeLocked(agent, thread); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	return s.writeResponse(req.ID, ChannelAgentStartResult{Agent: agent}, nil)
}

func (s *Server) handleChannelRoomList(ctx context.Context, req Request) error {
	if s == nil || s.channelService == nil {
		return s.writeResponse(req.ID, nil, errors.New("channels service is unavailable"))
	}
	rooms, err := s.channelService.ListRooms(ctx)
	return s.writeResponse(req.ID, ChannelRoomListResult{Rooms: rooms}, err)
}

func (s *Server) handleChannelRoomCreate(ctx context.Context, req Request) error {
	if s == nil || s.channelService == nil {
		return s.writeResponse(req.ID, nil, errors.New("channels service is unavailable"))
	}
	var params ChannelRoomCreateParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	members := []channels.RoomMember{{MemberType: channels.MemberHuman, MemberID: localChannelHumanID}}
	seen := make(map[string]struct{}, len(params.AgentIDs))
	for _, rawID := range params.AgentIDs {
		agentID := strings.TrimSpace(rawID)
		if agentID == "" {
			return s.writeResponse(req.ID, nil, errors.New("agent_ids cannot contain an empty id"))
		}
		if _, duplicate := seen[agentID]; duplicate {
			continue
		}
		seen[agentID] = struct{}{}
		members = append(members, channels.RoomMember{MemberType: channels.MemberAgent, MemberID: agentID})
	}
	room, err := s.channelService.CreateRoom(ctx, channels.CreateRoomParams{
		Name: params.Name, Kind: channels.RoomChannel, CreatedBy: localChannelHumanID, Members: members,
	})
	return s.writeResponse(req.ID, ChannelRoomCreateResult{Room: room}, err)
}

func (s *Server) handleChannelRoomDelete(ctx context.Context, req Request) error {
	if s == nil || s.channelService == nil {
		return s.writeResponse(req.ID, nil, errors.New("channels service is unavailable"))
	}
	var params ChannelRoomDeleteParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	err := s.channelService.DeleteRoom(ctx, params.RoomID)
	return s.writeResponse(req.ID, ChannelRoomDeleteResult{Deleted: err == nil}, err)
}

func (s *Server) handleChannelMessageList(ctx context.Context, req Request) error {
	if s == nil || s.channelService == nil {
		return s.writeResponse(req.ID, nil, errors.New("channels service is unavailable"))
	}
	var params ChannelMessageListParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	messages, err := s.channelService.ListMessages(ctx, params.RoomID, params.AfterSeq, params.Limit)
	return s.writeResponse(req.ID, ChannelMessageListResult{Messages: messages}, err)
}

func (s *Server) handleChannelMessageSend(ctx context.Context, req Request) error {
	if s == nil || s.channelService == nil {
		return s.writeResponse(req.ID, nil, errors.New("channels service is unavailable"))
	}
	var params ChannelMessageSendParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	images, err := normalizeTurnStartImages(params.Images)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	files, err := normalizeTurnStartFiles(params.Files)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	messageImages := make([]channels.MessageImage, 0, len(images))
	for _, image := range images {
		messageImages = append(messageImages, channels.MessageImage{
			MediaType: image.MediaType, Data: image.Data, Width: image.Width, Height: image.Height,
		})
	}
	messageFiles := make([]channels.MessageFile, 0, len(files))
	for _, file := range files {
		messageFiles = append(messageFiles, channels.MessageFile{
			MediaType: file.MediaType, Data: file.Data, Filename: file.Filename,
		})
	}
	result, err := s.channelService.SendHuman(ctx, channels.HumanSendParams{
		RoomID: params.RoomID, HumanID: localChannelHumanID, ThreadID: params.ThreadID, ReplyTo: params.ReplyTo, Body: params.Body,
		Images: messageImages, Files: messageFiles,
	})
	return s.writeResponse(req.ID, ChannelMessageSendResult{Message: result.Message}, err)
}

func (s *Server) handleChannelTaskCreate(ctx context.Context, req Request) error {
	if s == nil || s.channelService == nil {
		return s.writeResponse(req.ID, nil, errors.New("channels service is unavailable"))
	}
	var params ChannelTaskCreateParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	task, err := s.channelService.CreateTaskHuman(ctx, channels.TaskCreateParams{
		RoomID: params.RoomID, Title: params.Title, OwnerID: params.OwnerID, HumanID: localChannelHumanID,
	})
	return s.writeResponse(req.ID, ChannelTaskCreateResult{Task: task}, err)
}

func (s *Server) handleChannelTaskUpdate(ctx context.Context, req Request) error {
	if s == nil || s.channelService == nil {
		return s.writeResponse(req.ID, nil, errors.New("channels service is unavailable"))
	}
	var params ChannelTaskUpdateParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	task, err := s.channelService.UpdateTaskHuman(ctx, channels.TaskUpdateParams{
		TaskID: params.TaskID, State: channels.TaskState(params.State), OwnerID: params.OwnerID, HumanID: localChannelHumanID,
	})
	return s.writeResponse(req.ID, ChannelTaskUpdateResult{Task: task}, err)
}

func (s *Server) handleChannelHumanMentionStatus(ctx context.Context, req Request) error {
	if s == nil || s.channelService == nil {
		return s.writeResponse(req.ID, nil, errors.New("channels service is unavailable"))
	}
	counts, err := s.channelService.HumanMentionStatus(ctx, localChannelHumanID)
	total := 0
	for _, count := range counts {
		total += count.UnreadCount
	}
	return s.writeResponse(req.ID, ChannelHumanMentionStatusResult{Count: total}, err)
}

func (s *Server) handleChannelHumanMentionAck(ctx context.Context, req Request) error {
	if s == nil || s.channelService == nil {
		return s.writeResponse(req.ID, nil, errors.New("channels service is unavailable"))
	}
	counts, err := s.channelService.HumanMentionStatus(ctx, localChannelHumanID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	total := 0
	for _, count := range counts {
		if err := s.channelService.AckHumanMentions(ctx, count.RoomID, localChannelHumanID); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		total += count.UnreadCount
	}
	return s.writeResponse(req.ID, ChannelHumanMentionAckResult{Acknowledged: total}, nil)
}
