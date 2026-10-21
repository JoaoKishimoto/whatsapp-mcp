package wa

import (
	"testing"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestChangedRenameAndPeople(t *testing.T) {
	group := types.NewJID("120363000000000001", types.GroupServer)
	lid := types.NewJID("123456789", types.HiddenUserServer)
	pn := types.NewJID("5571999999999", types.DefaultUserServer)
	c, ok := Changed(&events.GroupInfo{
		JID: group, Sender: &lid, SenderPN: &pn,
		Name:  &types.GroupName{Name: "🥗 Dieta 2"},
		Join:  []types.JID{types.NewJID("5511888888888", types.DefaultUserServer)},
		Leave: []types.JID{lid},
	})
	if !ok || c.Name != "🥗 Dieta 2" || c.By == nil || c.By.PN != "5571999999999" || c.By.LID != "123456789@lid" {
		t.Fatalf("got %+v ok=%v", c, ok)
	}
	if len(c.Joined) != 1 || c.Joined[0].PN != "5511888888888" || len(c.Left) != 1 || c.Left[0].LID != "123456789@lid" {
		t.Errorf("people = %+v / %+v", c.Joined, c.Left)
	}
}

func TestChangedLinkAndDelete(t *testing.T) {
	community := types.NewJID("120363000000000009", types.GroupServer)
	c, ok := Changed(&events.GroupInfo{
		JID:    types.NewJID("120363000000000001", types.GroupServer),
		Unlink: &types.GroupLinkChange{Type: types.GroupLinkChangeTypeParent, UnlinkReason: types.GroupUnlinkReasonDefault, Group: types.GroupLinkTarget{JID: community}},
		Delete: &types.GroupDelete{Deleted: true},
	})
	if !ok || !c.Deleted || c.Unlinked == nil || c.Unlinked.JID != community.String() || c.Unlinked.Type != "parent_group" || c.Linked != nil {
		t.Errorf("got %+v ok=%v", c, ok)
	}
}

func TestChangedIgnoresSettings(t *testing.T) {
	link := "https://chat.whatsapp.com/x"
	if c, ok := Changed(&events.GroupInfo{Topic: &types.GroupTopic{Topic: "nova descrição"}, NewInviteLink: &link}); ok {
		t.Errorf("description/invite change forwarded: %+v", c)
	}
}

func TestCallOf(t *testing.T) {
	lid := types.NewJID("123456789", types.HiddenUserServer)
	pn := types.NewJID("5571999999999", types.DefaultUserServer)
	device := types.JID{User: "123456789", Device: 12, Server: types.HiddenUserServer}
	c := CallOf(&events.CallOffer{
		BasicCallMeta: types.BasicCallMeta{From: device, CallCreator: lid, CallCreatorAlt: pn, CallID: "CALL1"},
		Data:          &waBinary.Node{Tag: "offer", Content: []waBinary.Node{{Tag: "video"}}},
	})
	if c.CallID != "CALL1" || c.FromJID != "123456789@lid" || c.From.PN != "5571999999999" || !c.Video {
		t.Errorf("got %+v", c)
	}
	voice := CallOf(&events.CallOffer{BasicCallMeta: types.BasicCallMeta{From: device, CallID: "CALL2"}, Data: &waBinary.Node{Tag: "offer"}})
	if voice.Video || voice.FromJID != "123456789@lid" {
		t.Errorf("voice call without creator = %+v", voice)
	}
}
