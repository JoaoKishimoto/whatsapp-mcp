package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"whatsapp-client/internal/media"
	"whatsapp-client/internal/wa"
)

// MaxFileBytes caps sends and downloads: whole files pass through a 256 MB container.
const MaxFileBytes = 100 << 20

var (
	ErrMediaUnknown = errors.New("no attachment known for this message (never received, or older than the retention)")
	ErrMediaGone    = errors.New("the attachment is no longer on WhatsApp's servers")
	ErrMediaTooBig  = errors.New("the attachment is larger than 100 MB")
)

// Media is the attachment part of the WhatsApp client (PLANO.md §7, G9). Paths are
// already resolved inside the media directory.
type Media interface {
	DownloadMedia(ctx context.Context, chat types.JID, messageID string) (Downloaded, error)
	SendFile(ctx context.Context, to types.JID, path string, o media.Outgoing, reply *Reply) (messageID string, err error)
}

// Downloaded is a received attachment saved in the shared media volume.
type Downloaded struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	MIME     string `json:"mime,omitempty"`
	Filename string `json:"filename,omitempty"`
	Size     int64  `json:"size"`
}

// Reply makes a message quote an earlier one. An empty Sender means the bot wrote it.
type Reply struct {
	MessageID string
	Sender    types.JID
	Text      string
}

// replyReq is "reply_to" in a send request.
type replyReq struct {
	MessageID string `json:"message_id"`
	Sender    string `json:"sender"`  // who wrote it; required in a group unless from_me
	FromMe    bool   `json:"from_me"` // quoting one of the bot's own messages
	Text      string `json:"text"`    // shown in the quote preview (optional)
}

func (s *Server) routeMedia() {
	s.mux.Handle("POST /v1/media/download", s.require(scopeBridge, s.downloadMedia))
	s.mux.Handle("POST /v1/messages/file", s.require(scopeBridge, s.sendFile))
}

func (s *Server) downloadMedia(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Chat      string `json:"chat"`
		MessageID string `json:"message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.MessageID) == "" {
		writeError(w, http.StatusBadRequest, "chat and message_id are required")
		return
	}
	chat, err := parseChat(req.Chat)
	if err != nil {
		writeError(w, http.StatusBadRequest, "chat: "+err.Error())
		return
	}
	d, err := s.wa.DownloadMedia(r.Context(), chat, req.MessageID)
	switch {
	case errors.Is(err, ErrMediaUnknown):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrMediaGone):
		writeError(w, http.StatusGone, err.Error())
	case errors.Is(err, ErrMediaTooBig):
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
	case err != nil:
		waFailed(w, "download", err)
	default:
		writeJSON(w, http.StatusOK, d)
	}
}

func (s *Server) sendFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To         string    `json:"to"`
		Path       string    `json:"path"` // inside the media directory (absolute or relative to it)
		Caption    string    `json:"caption"`
		Filename   string    `json:"filename"`
		MIME       string    `json:"mime"`
		PTT        bool      `json:"ptt"`         // voice note (Ogg/Opus)
		AsDocument bool      `json:"as_document"` // images/videos without compression
		ReplyTo    *replyReq `json:"reply_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	to, err := parseChat(req.To)
	if err != nil {
		writeError(w, http.StatusBadRequest, "to: "+err.Error())
		return
	}
	path, err := media.Resolve(s.cfg.MediaDir, req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "path: "+err.Error())
		return
	}
	fi, err := os.Stat(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "path: "+err.Error())
		return
	}
	if fi.Size() > MaxFileBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file is larger than 100 MB")
		return
	}
	o := media.Outgoing{MIME: req.MIME, Filename: req.Filename, Caption: req.Caption, PTT: req.PTT, AsDocument: req.AsDocument}
	if o.Filename == "" {
		o.Filename = filepath.Base(path)
	}
	if o.MIME == "" {
		o.MIME = detectMIME(path)
	}
	if _, err := media.TypeFor(o); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	reply, ok := parseReply(w, to, req.ReplyTo)
	if !ok {
		return
	}
	id, err := s.wa.SendFile(r.Context(), to, path, o, reply)
	if err != nil {
		waFailed(w, "send", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message_id": id})
}

func detectMIME(path string) string {
	head := make([]byte, 512)
	if f, err := os.Open(path); err == nil {
		n, _ := io.ReadFull(f, head)
		head = head[:n]
		f.Close()
	}
	return media.DetectMIME(path, head)
}

// parseReply checks reply_to against the chat it is sent to; nil when absent.
func parseReply(w http.ResponseWriter, chat types.JID, req *replyReq) (*Reply, bool) {
	if req == nil {
		return nil, true
	}
	if strings.TrimSpace(req.MessageID) == "" {
		writeError(w, http.StatusBadRequest, "reply_to.message_id is required")
		return nil, false
	}
	reply := &Reply{MessageID: req.MessageID, Text: req.Text}
	switch {
	case req.FromMe:
		// Sender stays empty: the adapter fills in the bot's own JID.
	case req.Sender != "":
		people, ok := parsePeople(w, []string{req.Sender})
		if !ok {
			return nil, false
		}
		reply.Sender = people[0]
	case isUser(chat):
		reply.Sender = chat // in a DM, the other person wrote it
	default:
		writeError(w, http.StatusBadRequest, "reply_to.sender (or from_me) is required in a group")
		return nil, false
	}
	return reply, true
}

// Context is the quote to attach to the message (nil for no reply); own is the bot's
// JID, used when the bot wrote the quoted message.
func (r *Reply) Context(own types.JID) *waE2E.ContextInfo {
	if r == nil {
		return nil
	}
	sender := r.Sender
	if sender.IsEmpty() {
		sender = own
	}
	return wa.QuoteContext(r.MessageID, sender, r.Text)
}
