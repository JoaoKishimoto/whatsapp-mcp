// Package api is the gateway's internal REST API (PLANO.md §7).
// Two bearer tokens: bridge (everything) and notify (send text to the owner only).
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"whatsapp-client/internal/polls"
	"whatsapp-client/internal/queue"
)

const (
	maxWait        = 25 * time.Second
	pageSize       = 100
	maxBodyKB      = 1024
	maxPollOptions = 12 // WhatsApp's limit
)

type scope int

const (
	scopeNone scope = iota
	scopeNotify
	scopeBridge
)

type Health struct {
	Connected bool `json:"connected"`
	LoggedIn  bool `json:"logged_in"`
}

// WhatsApp is the part of the whatsmeow client the API needs.
type WhatsApp interface {
	SendText(ctx context.Context, to types.JID, text string, reply *Reply) (messageID string, err error)
	// SendPoll returns the poll's message id and the chat JID it went to (needed to match votes).
	SendPoll(ctx context.Context, to, question string, options []string, selectable int) (messageID, chatJID string, err error)
	// Lookup reports whether phone (digits) has WhatsApp, and the JID WhatsApp has for it.
	Lookup(ctx context.Context, phone string) (jid types.JID, onWhatsApp bool, err error)
	Health() Health
	Groups
	Chat
	Media
	Calls
}

type Config struct {
	BridgeToken string
	NotifyToken string
	OwnerPhone  string // digits only; the only destination for the notify token
	MediaDir    string // shared volume: downloads land here, files are sent only from here
}

type Server struct {
	cfg Config
	wa  WhatsApp
	q   *queue.Store
	p   *polls.Store
	mux *http.ServeMux
}

func New(cfg Config, wa WhatsApp, q *queue.Store, p *polls.Store) (*Server, error) {
	switch {
	case cfg.BridgeToken == "" || cfg.NotifyToken == "":
		return nil, errors.New("TOKEN_BRIDGE and TOKEN_NOTIFY are required")
	case cfg.BridgeToken == cfg.NotifyToken:
		return nil, errors.New("TOKEN_BRIDGE and TOKEN_NOTIFY must differ")
	case cfg.OwnerPhone == "":
		return nil, errors.New("OWNER_PHONE is required")
	case cfg.MediaDir == "":
		return nil, errors.New("a media directory is required")
	}
	s := &Server{cfg: cfg, wa: wa, q: q, p: p, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /v1/health", s.health)
	s.mux.Handle("GET /v1/events", s.require(scopeBridge, s.listEvents))
	s.mux.Handle("POST /v1/events/ack", s.require(scopeBridge, s.ackEvents))
	s.mux.Handle("POST /v1/messages", s.require(scopeNotify, s.sendMessage))
	s.mux.Handle("POST /v1/polls", s.require(scopeBridge, s.sendPoll))
	s.mux.Handle("POST /v1/contacts/lookup", s.require(scopeBridge, s.lookupContact))
	s.routeGroups()
	s.routeChat()
	s.routeMedia()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// BridgeOnly guards routes outside /v1 (the upstream /api/*) with the bridge token.
func (s *Server) BridgeOnly(h http.HandlerFunc) http.Handler { return s.require(scopeBridge, h) }

func (s *Server) scopeOf(r *http.Request) scope {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		return scopeNone
	}
	// Compare against both so timing does not reveal which one matched.
	isBridge := subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.BridgeToken)) == 1
	isNotify := subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.NotifyToken)) == 1
	switch {
	case isBridge:
		return scopeBridge
	case isNotify:
		return scopeNotify
	}
	return scopeNone
}

type ctxKey struct{}

func (s *Server) require(min scope, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := s.scopeOf(r)
		if got == scopeNone {
			writeError(w, http.StatusUnauthorized, "missing or invalid token")
			return
		}
		if got < min {
			writeError(w, http.StatusForbidden, "token not allowed for this route")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyKB*1024)
		h(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, got)))
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.wa.Health())
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	after, err1 := intParam(r, "after")
	wait, err2 := intParam(r, "wait")
	if err1 != nil || err2 != nil {
		writeError(w, http.StatusBadRequest, "after and wait must be non-negative integers")
		return
	}
	waitFor := min(time.Duration(wait)*time.Second, maxWait)
	evs, err := s.q.List(r.Context(), after, pageSize, waitFor)
	if err != nil {
		if r.Context().Err() == nil {
			writeError(w, http.StatusInternalServerError, "could not read events")
		}
		return
	}
	writeJSON(w, http.StatusOK, evs)
}

func (s *Server) ackEvents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UpTo int64 `json:"up_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UpTo <= 0 {
		writeError(w, http.StatusBadRequest, "up_to must be a positive event id")
		return
	}
	if err := s.q.Ack(req.UpTo); err != nil {
		writeError(w, http.StatusInternalServerError, "could not ack events")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To      string    `json:"to"`
		Text    string    `json:"text"`
		ReplyTo *replyReq `json:"reply_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if r.Context().Value(ctxKey{}) == scopeNotify {
		req.To = s.cfg.OwnerPhone // notify can only reach the owner; any "to" is ignored
	}
	to, err := parseChat(req.To)
	if err != nil {
		writeError(w, http.StatusBadRequest, "to: "+err.Error())
		return
	}
	reply, ok := parseReply(w, to, req.ReplyTo)
	if !ok {
		return
	}
	id, err := s.wa.SendText(r.Context(), to, req.Text, reply)
	if err != nil {
		waFailed(w, "send", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message_id": id})
}

func (s *Server) sendPoll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To         string   `json:"to"`
		Question   string   `json:"question"`
		Options    []string `json:"options"`
		Selectable int      `json:"selectable"` // how many options a voter may pick; 0 or absent = 1
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if msg := validatePoll(req.To, req.Question, req.Options, req.Selectable); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	selectable := max(req.Selectable, 1)
	id, chat, err := s.wa.SendPoll(r.Context(), req.To, req.Question, req.Options, selectable)
	if err != nil {
		waFailed(w, "send", err)
		return
	}
	if err := s.p.Save(id, chat, req.Options); err != nil {
		// The poll is out, but its votes cannot be read back. Say so instead of pretending it worked.
		writeError(w, http.StatusInternalServerError, "poll sent ("+id+") but its options could not be saved; votes will be ignored")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message_id": id})
}

// lookupContact checks a number before the first message to it. The JID can differ from
// the digits sent (an old Brazilian account without the extra 9): send to the JID.
func (s *Server) lookupContact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !plausiblePhone(req.Phone) {
		writeError(w, http.StatusBadRequest, "phone must be 10 to 15 digits, country code included")
		return
	}
	jid, ok, err := s.wa.Lookup(r.Context(), req.Phone)
	if err != nil {
		waFailed(w, "lookup", err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"on_whatsapp": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"on_whatsapp": true, "jid": jid.String()})
}

func plausiblePhone(s string) bool {
	if len(s) < 10 || len(s) > 15 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func validatePoll(to, question string, options []string, selectable int) string {
	switch {
	case to == "":
		return "to is required"
	case strings.TrimSpace(question) == "":
		return "question is required"
	case len(options) < 2 || len(options) > maxPollOptions:
		return "options must have 2 to 12 items"
	case selectable < 0 || selectable > len(options):
		return "selectable must be between 1 and the number of options"
	}
	seen := map[string]bool{}
	for _, o := range options {
		if strings.TrimSpace(o) == "" || seen[o] {
			return "options must be non-empty and different from each other"
		}
		seen[o] = true
	}
	return ""
}

func intParam(r *http.Request, name string) (int64, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err == nil && n < 0 {
		err = errors.New("negative")
	}
	return n, err
}

// waFailed reports a WhatsApp error: 429 when WhatsApp is rate-limiting this number
// (the caller must back off: retrying fast on an unofficial client risks a ban), else 502.
func waFailed(w http.ResponseWriter, what string, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, whatsmeow.ErrIQRateOverLimit) {
		status = http.StatusTooManyRequests
	}
	writeError(w, status, "WhatsApp "+what+" failed: "+err.Error())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
