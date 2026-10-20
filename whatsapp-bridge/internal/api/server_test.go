package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"whatsapp-client/internal/polls"
	"whatsapp-client/internal/queue"
)

const (
	bridgeTok = "bridge-secret"
	notifyTok = "notify-secret"
	owner     = "5571999999999"
)

type sent struct{ to, text string }

type sentPoll struct {
	to, question string
	options      []string
	selectable   int
}

type fakeWA struct {
	sent  []sent
	polls []sentPoll
	calls []call // group and chat methods record here (groups_test.go, chat_test.go)
	fail  bool
	err   error // returned by group methods when set
}

func (f *fakeWA) SendPoll(_ context.Context, to, question string, options []string, selectable int) (string, string, error) {
	if f.fail {
		return "", "", errors.New("not connected")
	}
	f.polls = append(f.polls, sentPoll{to, question, options, selectable})
	return "3EB0POLL", to + "@s.whatsapp.net", nil
}

func (f *fakeWA) SendText(_ context.Context, to, text string) (string, error) {
	if f.fail {
		return "", errors.New("not connected")
	}
	f.sent = append(f.sent, sent{to, text})
	return "3EB0SENT", nil
}

func (f *fakeWA) Health() Health { return Health{Connected: true, LoggedIn: true} }

func setup(t *testing.T) (*Server, *fakeWA, *queue.Store) {
	s, wa, q, _ := setupWithPolls(t)
	return s, wa, q
}

func setupWithPolls(t *testing.T) (*Server, *fakeWA, *queue.Store, *polls.Store) {
	t.Helper()
	q, err := queue.Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { q.Close() })
	p, err := polls.Open(filepath.Join(t.TempDir(), "polls.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	wa := &fakeWA{}
	s, err := New(Config{BridgeToken: bridgeTok, NotifyToken: notifyTok, OwnerPhone: owner}, wa, q, p)
	if err != nil {
		t.Fatal(err)
	}
	return s, wa, q, p
}

func do(s http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestNewRefusesBadConfig(t *testing.T) {
	for _, cfg := range []Config{
		{NotifyToken: "n", OwnerPhone: owner},
		{BridgeToken: "b", OwnerPhone: owner},
		{BridgeToken: "same", NotifyToken: "same", OwnerPhone: owner},
		{BridgeToken: "b", NotifyToken: "n"},
	} {
		if _, err := New(cfg, &fakeWA{}, nil, nil); err == nil {
			t.Errorf("accepted %+v", cfg)
		}
	}
}

func TestAuth(t *testing.T) {
	s, _, _ := setup(t)
	cases := []struct {
		name, method, path, token string
		want                      int
	}{
		{"no token", "GET", "/v1/events", "", 401},
		{"wrong token", "GET", "/v1/events", "nope", 401},
		{"notify on events", "GET", "/v1/events", notifyTok, 403},
		{"notify on ack", "POST", "/v1/events/ack", notifyTok, 403},
		{"bridge on events", "GET", "/v1/events", bridgeTok, 200},
		{"health is open", "GET", "/v1/health", "", 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if w := do(s, c.method, c.path, c.token, ""); w.Code != c.want {
				t.Errorf("got %d, want %d (%s)", w.Code, c.want, w.Body)
			}
		})
	}
}

func TestBearerPrefixRequired(t *testing.T) {
	s, _, _ := setup(t)
	r := httptest.NewRequest("GET", "/v1/events", nil)
	r.Header.Set("Authorization", bridgeTok) // no "Bearer "
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Errorf("got %d", w.Code)
	}
}

func TestNotifyAlwaysGoesToOwner(t *testing.T) {
	s, wa, _ := setup(t)
	w := do(s, "POST", "/v1/messages", notifyTok, `{"to":"5511888888888","text":"oi"}`)
	if w.Code != 200 {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	do(s, "POST", "/v1/messages", notifyTok, `{"text":"sem destino"}`)
	if len(wa.sent) != 2 || wa.sent[0].to != owner || wa.sent[1].to != owner {
		t.Errorf("sent = %+v, want both to owner", wa.sent)
	}
}

func TestBridgeSendsAnywhere(t *testing.T) {
	s, wa, _ := setup(t)
	w := do(s, "POST", "/v1/messages", bridgeTok, `{"to":"5511888888888","text":"oi"}`)
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != 200 || resp["message_id"] != "3EB0SENT" || wa.sent[0].to != "5511888888888" {
		t.Errorf("got %d %s sent=%+v", w.Code, w.Body, wa.sent)
	}
}

func TestSendValidation(t *testing.T) {
	s, wa, _ := setup(t)
	for _, body := range []string{`{"to":"55","text":"  "}`, `{"to":"55"}`, `not json`, `{"text":"oi"}`} {
		if w := do(s, "POST", "/v1/messages", bridgeTok, body); w.Code != 400 {
			t.Errorf("%s: got %d", body, w.Code)
		}
	}
	if len(wa.sent) != 0 {
		t.Errorf("sent %+v", wa.sent)
	}
}

func TestSendFailureIs502(t *testing.T) {
	s, wa, _ := setup(t)
	wa.fail = true
	if w := do(s, "POST", "/v1/messages", notifyTok, `{"text":"oi"}`); w.Code != 502 {
		t.Errorf("got %d", w.Code)
	}
}

func TestEventsAndAck(t *testing.T) {
	s, _, q := setup(t)
	q.Append("message", map[string]string{"text": "a"})
	q.Append("message", map[string]string{"text": "b"})

	w := do(s, "GET", "/v1/events?after=1", bridgeTok, "")
	var evs []queue.Event
	json.Unmarshal(w.Body.Bytes(), &evs)
	if w.Code != 200 || len(evs) != 1 || evs[0].ID != 2 || string(evs[0].Data) != `{"text":"b"}` {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", "/v1/events/ack", bridgeTok, `{"up_to":2}`); w.Code != 204 {
		t.Errorf("ack got %d", w.Code)
	}
	if w := do(s, "GET", "/v1/events?after=2", bridgeTok, ""); w.Body.String() != "[]\n" {
		t.Errorf("empty list = %q, want []", w.Body)
	}
}

func TestEventParamValidation(t *testing.T) {
	s, _, _ := setup(t)
	for _, path := range []string{"/v1/events?after=-1", "/v1/events?after=x", "/v1/events?wait=-5"} {
		if w := do(s, "GET", path, bridgeTok, ""); w.Code != 400 {
			t.Errorf("%s: got %d", path, w.Code)
		}
	}
	if w := do(s, "POST", "/v1/events/ack", bridgeTok, `{"up_to":0}`); w.Code != 400 {
		t.Errorf("ack 0: got %d", w.Code)
	}
}

func TestBridgeOnlyGuardsLegacyRoutes(t *testing.T) {
	s, _, _ := setup(t)
	called := false
	h := s.BridgeOnly(func(w http.ResponseWriter, r *http.Request) { called = true })
	if w := do(h, "POST", "/api/send", notifyTok, ""); w.Code != 403 || called {
		t.Errorf("notify reached legacy route: %d", w.Code)
	}
	if do(h, "POST", "/api/send", bridgeTok, ""); !called {
		t.Error("bridge token blocked")
	}
}

func TestPollSentAndOptionsSaved(t *testing.T) {
	s, wa, _, p := setupWithPolls(t)
	w := do(s, "POST", "/v1/polls", bridgeTok, `{"to":"5571999999999","question":"Modelo?","options":["Haiku","Sonnet"]}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "3EB0POLL") {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	if len(wa.polls) != 1 || wa.polls[0].selectable != 1 {
		t.Errorf("polls = %+v, want one with selectable defaulting to 1", wa.polls)
	}
	if opts, ok, _ := p.Options("3EB0POLL"); !ok || len(opts) != 2 || opts[1] != "Sonnet" {
		t.Errorf("saved options = %v ok=%v", opts, ok)
	}
}

func TestPollNeedsBridgeToken(t *testing.T) {
	s, wa, _ := setup(t)
	w := do(s, "POST", "/v1/polls", notifyTok, `{"to":"55","question":"q","options":["a","b"]}`)
	if w.Code != 403 || len(wa.polls) != 0 {
		t.Errorf("got %d, polls=%v", w.Code, wa.polls)
	}
}

func TestPollValidation(t *testing.T) {
	s, wa, _ := setup(t)
	for _, body := range []string{
		`{"question":"q","options":["a","b"]}`,
		`{"to":"55","question":" ","options":["a","b"]}`,
		`{"to":"55","question":"q","options":["a"]}`,
		`{"to":"55","question":"q","options":["1","2","3","4","5","6","7","8","9","10","11","12","13"]}`,
		`{"to":"55","question":"q","options":["a","a"]}`,
		`{"to":"55","question":"q","options":["a",""]}`,
		`{"to":"55","question":"q","options":["a","b"],"selectable":3}`,
		`{"to":"55","question":"q","options":["a","b"],"selectable":-1}`,
	} {
		if w := do(s, "POST", "/v1/polls", bridgeTok, body); w.Code != 400 {
			t.Errorf("%s: got %d", body, w.Code)
		}
	}
	if len(wa.polls) != 0 {
		t.Errorf("sent %v", wa.polls)
	}
}
