package tasks

import (
	"limmr/internal/model"
	"limmr/internal/wa"
	"strings"
)

// KeepWithOwnerRequest is the MCP promotion path. Owner controls use Keep directly.
func (s *Service) KeepWithOwnerRequest(a *model.Agent, id string, request OwnerRequest) (Task, error) {
	ask, err := s.ownerRequest(a, &request)
	if err != nil {
		return Task{}, err
	}
	return s.resolve(a, id, "kept", "", ask)
}

func (s *Service) ownerRequest(a *model.Agent, request *OwnerRequest) (*AskedByOwner, error) {
	if request == nil {
		return nil, nil
	}
	if strings.TrimSpace(request.Text) == "" || !length(request.Text, 500) {
		return nil, model.ErrInvalid
	}
	if request.MessageRef != "" {
		if s.Hub.Messages == nil {
			return nil, model.ErrInvalid
		}
		_, visible, err := s.Hub.Visible(a)
		if err != nil {
			return nil, err
		}
		// IDs from read/find identify messages. Refuse ambiguous IDs as well as deleted,
		// inaccessible and non-owner messages; a recorded AI send is never an owner ask.
		messages, err := s.Hub.Messages.Messages(wa.Query{ID: request.MessageRef, Limit: 2}, visible)
		if err != nil {
			return nil, err
		}
		if len(messages) != 1 || messages[0].DeletedAt != nil {
			return nil, model.ErrInvalid
		}
		message, err := s.Hub.Attribute(messages[0])
		if err != nil {
			return nil, err
		}
		if message.Author.Type != "owner" {
			return nil, model.ErrInvalid
		}
	}
	agentID := "you"
	if a != nil {
		agentID = a.ID
	}
	return &AskedByOwner{OwnerRequest: *request, AgentID: agentID, At: s.now()}, nil
}

func ownerRequestLine(line string, ask *AskedByOwner) string {
	if ask == nil {
		return line
	}
	return line + ": “" + ask.Text + "”"
}
