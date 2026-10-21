package api

import (
	"context"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

func (f *fakeWA) SetTyping(_ context.Context, chat types.JID, typing bool) error {
	return f.record("typing", chat, " ", typing)
}

func (f *fakeWA) React(_ context.Context, chat, sender types.JID, id, emoji string) (string, error) {
	return "3EB0REACT", f.record("react", chat, " ", sender, " ", id, " ", emoji)
}

func (f *fakeWA) MarkRead(_ context.Context, chat, sender types.JID, ids []string) error {
	return f.record("read", chat, " ", sender, " ", ids)
}

func (f *fakeWA) RejectCall(_ context.Context, from types.JID, callID string) error {
	return f.record("reject", from, " ", callID)
}

func TestRejectCall(t *testing.T) {
	s, f, _ := setup(t)
	if w := do(s, "POST", "/v1/calls/reject", bridgeTok, `{"from":"123456789@lid","call_id":"CALL1"}`); w.Code != 204 {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	if f.calls[0].args != "123456789@lid CALL1" {
		t.Errorf("args = %q", f.calls[0].args)
	}
	for _, body := range []string{`{"from":"123456789@lid"}`, `{"from":"` + topicJID + `","call_id":"C"}`, `{"call_id":"C"}`} {
		if w := do(s, "POST", "/v1/calls/reject", bridgeTok, body); w.Code != 400 {
			t.Errorf("%s: got %d", body, w.Code)
		}
	}
}

func TestChatRoutesNeedBridgeToken(t *testing.T) {
	s, wa, _ := setup(t)
	for _, path := range []string{"/v1/presence", "/v1/reactions", "/v1/read", "/v1/calls/reject"} {
		if w := do(s, "POST", path, notifyTok, `{}`); w.Code != 403 {
			t.Errorf("%s with notify token: got %d", path, w.Code)
		}
	}
	if len(wa.calls) != 0 {
		t.Errorf("WhatsApp was called: %v", wa.calls)
	}
}

func TestPresence(t *testing.T) {
	s, f, _ := setup(t)
	for _, body := range []string{
		`{"chat":"5571999999999","state":"composing"}`,
		`{"chat":"` + topicJID + `","state":"paused"}`,
	} {
		if w := do(s, "POST", "/v1/presence", bridgeTok, body); w.Code != 204 {
			t.Errorf("%s: got %d %s", body, w.Code, w.Body)
		}
	}
	want := []string{"5571999999999@s.whatsapp.net true", topicJID + " false"}
	for i, c := range f.calls {
		if c.args != want[i] {
			t.Errorf("call %d = %q, want %q", i, c.args, want[i])
		}
	}
}

func TestReactInGroupNeedsSender(t *testing.T) {
	s, f, _ := setup(t)
	body := `{"chat":"` + topicJID + `","message_id":"3EB0AAA","emoji":"👍"}`
	if w := do(s, "POST", "/v1/reactions", bridgeTok, body); w.Code != 400 || !strings.Contains(w.Body.String(), "sender") {
		t.Errorf("got %d %s", w.Code, w.Body)
	}
	body = `{"chat":"` + topicJID + `","message_id":"3EB0AAA","sender":"123456789@lid","emoji":"👍"}`
	w := do(s, "POST", "/v1/reactions", bridgeTok, body)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "3EB0REACT") {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	if got := f.calls[0].args; got != topicJID+" 123456789@lid 3EB0AAA 👍" {
		t.Errorf("args = %q", got)
	}
}

func TestReactInDMDefaultsSenderToChat(t *testing.T) {
	s, f, _ := setup(t)
	if w := do(s, "POST", "/v1/reactions", bridgeTok, `{"chat":"5571999999999","message_id":"3EB0AAA","emoji":""}`); w.Code != 200 {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	// Empty emoji removes the reaction; the sender is the other person in the DM.
	if got := f.calls[0].args; got != "5571999999999@s.whatsapp.net 5571999999999@s.whatsapp.net 3EB0AAA " {
		t.Errorf("args = %q", got)
	}
}

func TestMarkRead(t *testing.T) {
	s, f, _ := setup(t)
	body := `{"chat":"` + topicJID + `","message_ids":["3EB0A","3EB0B"],"sender":"5511888888888"}`
	if w := do(s, "POST", "/v1/read", bridgeTok, body); w.Code != 204 {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	if got := f.calls[0].args; got != topicJID+" 5511888888888@s.whatsapp.net [3EB0A 3EB0B]" {
		t.Errorf("args = %q", got)
	}
}

func TestChatValidation(t *testing.T) {
	s, f, _ := setup(t)
	cases := []struct{ path, body string }{
		{"/v1/presence", `{"chat":"5571999999999","state":"recording"}`},
		{"/v1/presence", `{"chat":"","state":"composing"}`},
		{"/v1/presence", `{"chat":"status@broadcast","state":"composing"}`},
		{"/v1/reactions", `{"chat":"5571999999999","message_id":"3EB0AAA"}`},
		{"/v1/reactions", `{"chat":"5571999999999","emoji":"👍"}`},
		{"/v1/reactions", `{"chat":"5571999999999","message_id":"3EB0AAA","sender":"` + topicJID + `","emoji":"👍"}`},
		{"/v1/read", `{"chat":"5571999999999","message_ids":[]}`},
		{"/v1/read", `{"chat":"5571999999999","message_ids":[""]}`},
		{"/v1/read", `{"chat":"` + topicJID + `","message_ids":["3EB0A"]}`},
		{"/v1/read", `not json`},
	}
	for _, c := range cases {
		if w := do(s, "POST", c.path, bridgeTok, c.body); w.Code != 400 {
			t.Errorf("%s %s: got %d", c.path, c.body, w.Code)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("WhatsApp was called: %v", f.calls)
	}
}

func TestChatErrors(t *testing.T) {
	s, f, _ := setup(t)
	f.err = whatsmeow.ErrIQRateOverLimit
	if w := do(s, "POST", "/v1/presence", bridgeTok, `{"chat":"5571999999999","state":"composing"}`); w.Code != 429 {
		t.Errorf("rate limit: got %d", w.Code)
	}
	f.err = nil
	f.fail = true
	if w := do(s, "POST", "/v1/read", bridgeTok, `{"chat":"5571999999999","message_ids":["3EB0A"]}`); w.Code != 502 {
		t.Errorf("not connected: got %d", w.Code)
	}
}
