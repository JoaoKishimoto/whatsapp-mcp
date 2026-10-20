package wa

import (
	"crypto/sha256"
	"reflect"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func hash(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }

func TestSelectedOptions(t *testing.T) {
	options := []string{"Haiku", "Sonnet", "Opus", "🥗 Dieta"}
	cases := []struct {
		name   string
		hashes [][]byte
		want   []string
	}{
		{"one", [][]byte{hash("Sonnet")}, []string{"Sonnet"}},
		{"several, in poll order", [][]byte{hash("Opus"), hash("Haiku")}, []string{"Haiku", "Opus"}},
		{"emoji option", [][]byte{hash("🥗 Dieta")}, []string{"🥗 Dieta"}},
		{"unchecked vote", nil, []string{}},
		{"unknown and malformed hashes ignored", [][]byte{hash("GPT"), {1, 2, 3}}, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SelectedOptions(options, c.hashes); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %#v, want %#v", got, c.want)
			}
		})
	}
}

func TestVote(t *testing.T) {
	at := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: lid, Sender: lid, SenderAlt: phone},
			Timestamp:     at,
		},
		Message: &waE2E.Message{PollUpdateMessage: &waE2E.PollUpdateMessage{
			PollCreationMessageKey: &waCommon.MessageKey{ID: proto.String("3EB0POLL")},
		}},
	}
	got := Vote(evt, []string{"Haiku", "Sonnet"}, [][]byte{hash("Haiku")})
	want := PollVote{
		PollMessageID: "3EB0POLL",
		ChatJID:       "123456789@lid",
		Voter:         Person{PN: "5571999999999", LID: "123456789@lid"},
		Selected:      []string{"Haiku"},
		Timestamp:     at,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestPollUpdateIsNotAMessageEvent(t *testing.T) {
	evt := event(types.MessageSource{Chat: phone, Sender: phone},
		&waE2E.Message{PollUpdateMessage: &waE2E.PollUpdateMessage{}})
	if _, ok := Normalize(evt); ok {
		t.Error("a poll vote must not also be queued as a message")
	}
}
