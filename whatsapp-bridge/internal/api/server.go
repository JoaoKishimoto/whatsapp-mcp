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

	"whatsapp-client/internal/queue"
)

const (
	maxWait   = 25 * time.Second
	pageSize  = 100
	maxBodyKB = 1024
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
	SendText(ctx context.Context, to, text string) (messageID string, err error)
	Health() Health
}

type Config struct {
	BridgeToken string
	NotifyToken string
	OwnerPhone  string // digits only; the only destination for the notify token
}

type Server struct {
	cfg Config
	wa  WhatsApp
	q   *queue.Store
	mux *http.ServeMux
}

func New(cfg Config, wa WhatsApp, q *queue.Store) (*Server, error) {
	switch {
	case cfg.BridgeToken == "" || cfg.NotifyToken == "":
		return nil, errors.New("TOKEN_BRIDGE and TOKEN_NOTIFY are required")
	case cfg.BridgeToken == cfg.NotifyToken:
		return nil, errors.New("TOKEN_BRIDGE and TOKEN_NOTIFY must differ")
	case cfg.OwnerPhone == "":
		return nil, errors.New("OWNER_PHONE is required")
	}
	s := &Server{cfg: cfg, wa: wa, q: q, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /v1/health", s.health)
	s.mux.Handle("GET /v1/events", s.require(scopeBridge, s.listEvents))
	s.mux.Handle("POST /v1/events/ack", s.require(scopeBridge, s.ackEvents))
	s.mux.Handle("POST /v1/messages", s.require(scopeNotify, s.sendMessage))
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
		To   string `json:"to"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if r.Context().Value(ctxKey{}) == scopeNotify {
		req.To = s.cfg.OwnerPhone // notify can only reach the owner; any "to" is ignored
	}
	if req.To == "" {
		writeError(w, http.StatusBadRequest, "to is required")
		return
	}
	id, err := s.wa.SendText(r.Context(), req.To, req.Text)
	if err != nil {
		writeError(w, http.StatusBadGateway, "WhatsApp send failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message_id": id})
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
