package wa

import (
	"reflect"
	"testing"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

var community = types.NewJID("120363000000000009", types.GroupServer)

func TestGroupKinds(t *testing.T) {
	cases := []struct {
		name string
		info types.GroupInfo
		kind string
		par  string
	}{
		{"community parent", types.GroupInfo{JID: community, GroupParent: types.GroupParent{IsParent: true}}, "community", ""},
		{"announcement channel", types.GroupInfo{JID: group,
			GroupIsDefaultSub: types.GroupIsDefaultSub{IsDefaultSubGroup: true},
			GroupLinkedParent: types.GroupLinkedParent{LinkedParentJID: community}}, "announcement", community.String()},
		{"topic group in community", types.GroupInfo{JID: group,
			GroupLinkedParent: types.GroupLinkedParent{LinkedParentJID: community}}, "group", community.String()},
		{"group outside any community", types.GroupInfo{JID: group}, "group", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := GroupFrom(&c.info)
			if g.Kind != c.kind || g.ParentJID != c.par {
				t.Errorf("got kind=%q parent=%q, want %q %q", g.Kind, g.ParentJID, c.kind, c.par)
			}
		})
	}
}

func TestParticipants(t *testing.T) {
	got := Participants([]types.GroupParticipant{
		{JID: lid, PhoneNumber: phone, LID: lid, IsSuperAdmin: true},
		{JID: phone, IsAdmin: false},
		{JID: lid, Error: 403},
	})
	want := []Participant{
		{Person: Person{PN: "5571999999999", LID: "123456789@lid"}, IsAdmin: true},
		{Person: Person{PN: "5571999999999"}},
		{Person: Person{LID: "123456789@lid"}, Error: 403},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestJoined(t *testing.T) {
	evt := &events.JoinedGroup{Sender: &lid, SenderPN: &phone, GroupInfo: types.GroupInfo{
		JID: group, GroupName: types.GroupName{Name: "🥗 Dieta"},
		GroupLinkedParent: types.GroupLinkedParent{LinkedParentJID: community},
	}}
	j := Joined(evt)
	if j.JID != group.String() || j.Name != "🥗 Dieta" || j.ParentJID != community.String() {
		t.Errorf("got %+v", j)
	}
	if j.AddedBy == nil || *j.AddedBy != (Person{PN: "5571999999999", LID: "123456789@lid"}) {
		t.Errorf("added_by = %+v", j.AddedBy)
	}
	if Joined(&events.JoinedGroup{GroupInfo: types.GroupInfo{JID: group}}).AddedBy != nil {
		t.Error("added_by should be absent when WhatsApp does not say who added the bot")
	}
}

func TestApplyGroup(t *testing.T) {
	msg, _ := Normalize(event(types.MessageSource{Chat: group, IsGroup: true, Sender: lid, SenderAlt: phone}, textBody))
	ApplyGroup(&msg, Group{Kind: "group", ParentJID: community.String()})
	if msg.ChatKind != "group" || msg.GroupParentJID != community.String() {
		t.Errorf("got %+v", msg)
	}
}

func TestParseRecipient(t *testing.T) {
	ok := map[string]string{
		"5571999999999":           "5571999999999@s.whatsapp.net",
		" 5571999999999 ":         "5571999999999@s.whatsapp.net",
		"123456789@lid":           "123456789@lid",
		"120363000000000001@g.us": "120363000000000001@g.us",
	}
	for in, want := range ok {
		if jid, err := ParseRecipient(in); err != nil || jid.String() != want {
			t.Errorf("%q: got %v, %v", in, jid, err)
		}
	}
	for _, in := range []string{"", "+55 71 9999", "55-71"} {
		if _, err := ParseRecipient(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestParseGroupJID(t *testing.T) {
	if _, err := ParseGroupJID("120363000000000001@g.us"); err != nil {
		t.Error(err)
	}
	for _, in := range []string{"5571999999999@s.whatsapp.net", "@g.us", "abc", ""} {
		if _, err := ParseGroupJID(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}
