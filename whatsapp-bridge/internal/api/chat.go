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

const maxReadIDs = 100

// Chat is the "typing…", reaction and read-receipt part of the WhatsApp client (PLANO.md §7, G8).
// sender is who sent the target message: in a group it is that participant, in a DM the chat itself.
type Chat interface {
	SetTyping(ctx context.Context, chat types.JID, typing bool) error
	// React sends emoji as a reaction to messageID; an empty emoji removes the reaction.
	React(ctx context.Context, chat, sender types.JID, messageID, emoji string) (reactionID string, err error)
	// MarkRead marks messages as read; all of them must be from the same sender.
	MarkRead(ctx context.Context, chat, sender types.JID, messageIDs []string) error
}

// Calls lets the bridge reject a call it was told about by a "call" event.
type Calls interface {
	RejectCall(ctx context.Context, from types.JID, callID string) error
}

func (s *Server) routeChat() {
	s.mux.Handle("POST /v1/presence", s.require(scopeBridge, s.presence))
	s.mux.Handle("POST /v1/reactions", s.require(scopeBridge, s.react))
	s.mux.Handle("POST /v1/read", s.require(scopeBridge, s.markRead))
	s.mux.Handle("POST /v1/calls/reject", s.require(scopeBridge, s.rejectCall))
}

func (s *Server) rejectCall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From   string `json:"from"` // from_jid of the call event
		CallID string `json:"call_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.CallID) == "" {
		writeError(w, http.StatusBadRequest, "from and call_id are required")
		return
	}
	people, ok := parsePeople(w, []string{req.From})
	if !ok {
		return
	}
	s.noContent(w, s.wa.RejectCall(r.Context(), people[0], req.CallID), "call reject")
}

func (s *Server) presence(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Chat  string `json:"chat"`
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.State != "composing" && req.State != "paused" {
		writeError(w, http.StatusBadRequest, `state must be "composing" or "paused"`)
		return
	}
	chat, err := parseChat(req.Chat)
	if err != nil {
		writeError(w, http.StatusBadRequest, "chat: "+err.Error())
		return
	}
	s.noContent(w, s.wa.SetTyping(r.Context(), chat, req.State == "composing"), "presence")
}

func (s *Server) react(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Chat      string  `json:"chat"`
		MessageID string  `json:"message_id"`
		Sender    string  `json:"sender"`
		Emoji     *string `json:"emoji"` // "" removes the reaction, so absent and empty differ
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.MessageID) == "" || req.Emoji == nil {
		writeError(w, http.StatusBadRequest, `message_id and emoji are required (emoji "" removes the reaction)`)
		return
	}
	chat, sender, ok := chatAndSender(w, req.Chat, req.Sender)
	if !ok {
		return
	}
	id, err := s.wa.React(r.Context(), chat, sender, req.MessageID, *req.Emoji)
	if err != nil {
		waFailed(w, "reaction", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message_id": id})
}

func (s *Server) markRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Chat       string   `json:"chat"`
		MessageIDs []string `json:"message_ids"`
		Sender     string   `json:"sender"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(req.MessageIDs) == 0 || len(req.MessageIDs) > maxReadIDs {
		writeError(w, http.StatusBadRequest, "message_ids must have 1 to 100 items")
		return
	}
	for _, id := range req.MessageIDs {
		if strings.TrimSpace(id) == "" {
			writeError(w, http.StatusBadRequest, "message_ids must not be empty")
			return
		}
	}
	chat, sender, ok := chatAndSender(w, req.Chat, req.Sender)
	if !ok {
		return
	}
	s.noContent(w, s.wa.MarkRead(r.Context(), chat, sender, req.MessageIDs), "read")
}

// parseChat accepts a DM (phone digits, phone or LID JID) or a group.
func parseChat(s string) (types.JID, error) {
	jid, err := wa.ParseRecipient(s)
	if err != nil {
		return jid, err
	}
	if !isUser(jid) && (jid.Server != types.GroupServer || jid.User == "") {
		return jid, errors.New("must be a phone number, a user JID or a group JID")
	}
	return jid, nil
}

// chatAndSender parses both; sender is required in a group and defaults to the chat in a DM.
func chatAndSender(w http.ResponseWriter, chatIn, senderIn string) (chat, sender types.JID, ok bool) {
	chat, err := parseChat(chatIn)
	if err != nil {
		writeError(w, http.StatusBadRequest, "chat: "+err.Error())
		return chat, sender, false
	}
	if senderIn == "" {
		if !isUser(chat) {
			writeError(w, http.StatusBadRequest, "sender is required in a group")
			return chat, sender, false
		}
		return chat, chat, true
	}
	people, ok := parsePeople(w, []string{senderIn})
	if !ok {
		return chat, sender, false
	}
	return chat, people[0], true
}

func isUser(jid types.JID) bool {
	return jid.User != "" && (jid.Server == types.DefaultUserServer || jid.Server == types.HiddenUserServer)
}
