package appserver

import (
	"fmt"

	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/session"
)

const maxNamedParticipants = 8

func (s *Server) ensureNamedParticipantCapacity() error {
	participants, err := session.ListParticipants(s.rt.SessionDir, participant.KindNamed)
	if err != nil {
		return fmt.Errorf("check named agent roster capacity: %w", err)
	}
	if len(participants) >= maxNamedParticipants {
		return fmt.Errorf("named agent roster is full (%d of %d); retire an agent before creating another", len(participants), maxNamedParticipants)
	}
	return nil
}
