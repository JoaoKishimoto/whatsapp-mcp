package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"go.mau.fi/whatsmeow/types"

	"whatsapp-client/internal/wa"
)

// Groups is the group part of the WhatsApp client (PLANO.md §7, G7). Inputs are already validated.
type Groups interface {
	// CreateGroup creates a community (community=true, with description) or a group, linked to parent when set.
	CreateGroup(ctx context.Context, name, description string, participants []types.JID, parent types.JID, community bool) (wa.Group, error)
	GroupInfo(ctx context.Context, jid types.JID) (wa.Group, error)
	// UpdateParticipants applies action ("add" or "promote") and returns one result per participant.
	UpdateParticipants(ctx context.Context, jid types.JID, action string, participants []types.JID) ([]wa.Participant, error)
	InviteLink(ctx context.Context, jid types.JID, reset bool) (string, error)
	LinkGroup(ctx context.Context, parent, child types.JID) error
	UnlinkGroup(ctx context.Context, child types.JID) error
	LeaveGroup(ctx context.Context, jid types.JID) error
}

func (s *Server) routeGroups() {
	s.mux.Handle("POST /v1/groups", s.require(scopeBridge, s.createGroup))
	s.mux.Handle("GET /v1/groups/{jid}", s.require(scopeBridge, s.groupInfo))
	s.mux.Handle("POST /v1/groups/{jid}/participants", s.require(scopeBridge, s.groupParticipants))
	s.mux.Handle("GET /v1/groups/{jid}/invite", s.require(scopeBridge, s.groupInvite))
	s.mux.Handle("POST /v1/groups/{jid}/link", s.require(scopeBridge, s.groupLink))
	s.mux.Handle("POST /v1/groups/{jid}/unlink", s.require(scopeBridge, s.groupUnlink))
	s.mux.Handle("POST /v1/groups/{jid}/leave", s.require(scopeBridge, s.groupLeave))
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string   `json:"name"`
		Description  string   `json:"description"` // communities only
		Participants []string `json:"participants"`
		Parent       string   `json:"parent"`
		Community    bool     `json:"community"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Community && req.Parent != "" {
		writeError(w, http.StatusBadRequest, "a community cannot have a parent")
		return
	}
	if !req.Community && req.Description != "" {
		writeError(w, http.StatusBadRequest, "description is only supported for communities")
		return
	}
	var parent types.JID
	if req.Parent != "" {
		var err error
		if parent, err = wa.ParseGroupJID(req.Parent); err != nil {
			writeError(w, http.StatusBadRequest, "parent: "+err.Error())
			return
		}
	}
	people, ok := parsePeople(w, req.Participants)
	if !ok {
		return
	}
	g, err := s.wa.CreateGroup(r.Context(), req.Name, req.Description, people, parent, req.Community)
	if err != nil {
		waFailed(w, "create group", err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) groupInfo(w http.ResponseWriter, r *http.Request) {
	jid, ok := groupJID(w, r)
	if !ok {
		return
	}
	g, err := s.wa.GroupInfo(r.Context(), jid)
	if err != nil {
		waFailed(w, "group info", err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) groupParticipants(w http.ResponseWriter, r *http.Request) {
	jid, ok := groupJID(w, r)
	if !ok {
		return
	}
	var req struct {
		Action       string   `json:"action"`
		Participants []string `json:"participants"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Action != "add" && req.Action != "promote" {
		writeError(w, http.StatusBadRequest, `action must be "add" or "promote"`)
		return
	}
	if len(req.Participants) == 0 {
		writeError(w, http.StatusBadRequest, "participants is required")
		return
	}
	people, ok := parsePeople(w, req.Participants)
	if !ok {
		return
	}
	res, err := s.wa.UpdateParticipants(r.Context(), jid, req.Action, people)
	if err != nil {
		waFailed(w, "participants update", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"participants": res})
}

func (s *Server) groupInvite(w http.ResponseWriter, r *http.Request) {
	jid, ok := groupJID(w, r)
	if !ok {
		return
	}
	link, err := s.wa.InviteLink(r.Context(), jid, r.URL.Query().Get("reset") == "true")
	if err != nil {
		waFailed(w, "invite link", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"link": link})
}

func (s *Server) groupLink(w http.ResponseWriter, r *http.Request) {
	child, ok := groupJID(w, r)
	if !ok {
		return
	}
	var req struct {
		Parent string `json:"parent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	parent, err := wa.ParseGroupJID(req.Parent)
	if err != nil {
		writeError(w, http.StatusBadRequest, "parent: "+err.Error())
		return
	}
	s.noContent(w, s.wa.LinkGroup(r.Context(), parent, child), "link")
}

func (s *Server) groupUnlink(w http.ResponseWriter, r *http.Request) {
	if jid, ok := groupJID(w, r); ok {
		s.noContent(w, s.wa.UnlinkGroup(r.Context(), jid), "unlink")
	}
}

func (s *Server) groupLeave(w http.ResponseWriter, r *http.Request) {
	if jid, ok := groupJID(w, r); ok {
		s.noContent(w, s.wa.LeaveGroup(r.Context(), jid), "leave")
	}
}

func (s *Server) noContent(w http.ResponseWriter, err error, what string) {
	if err != nil {
		waFailed(w, what, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func groupJID(w http.ResponseWriter, r *http.Request) (types.JID, bool) {
	jid, err := wa.ParseGroupJID(r.PathValue("jid"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return jid, false
	}
	return jid, true
}

// parsePeople accepts phone digits or user JIDs (phone or LID), never groups.
func parsePeople(w http.ResponseWriter, in []string) ([]types.JID, bool) {
	out := make([]types.JID, 0, len(in))
	for _, s := range in {
		jid, err := wa.ParseRecipient(s)
		if err == nil && !isUser(jid) {
			err = errors.New("must be a phone number or a user JID")
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "participant "+s+": "+err.Error())
			return nil, false
		}
		out = append(out, jid)
	}
	return out, true
}
