package wa

import (
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types/events"
)

func TestConnectionOf(t *testing.T) {
	cases := []struct {
		evt    any
		state  string
		reason string // substring
		fatal  bool
	}{
		{&events.Connected{}, "connected", "", false},
		{&events.Disconnected{}, "disconnected", "", false},
		{&events.LoggedOut{Reason: events.ConnectFailureLoggedOut}, "logged_out", "logged out", true},
		{&events.TemporaryBan{Code: events.TempBanSentToTooManyPeople, Expire: time.Hour}, "temporary_ban", "1h0m0s", true},
		{&events.ClientOutdated{}, "client_outdated", "", true},
		{&events.StreamReplaced{}, "stream_replaced", "", true},
		{&events.ConnectFailure{Reason: events.ConnectFailureBadUserAgent, Message: "bad ua"}, "connect_failure", "bad ua", true},
	}
	for _, c := range cases {
		got, ok := ConnectionOf(c.evt)
		if !ok || got.State != c.state || !strings.Contains(got.Reason, c.reason) || got.Fatal() != c.fatal {
			t.Errorf("%T: got %+v ok=%v fatal=%v", c.evt, got, ok, got.Fatal())
		}
	}
	if _, ok := ConnectionOf(&events.Message{}); ok {
		t.Error("a message is not a connection event")
	}
}
