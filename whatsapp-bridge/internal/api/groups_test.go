package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"whatsapp-client/internal/wa"
)

const (
	communityJID = "120363000000000009@g.us"
	topicJID     = "120363000000000001@g.us"
)

type call struct {
	method string
	args   string
}

func (f *fakeWA) record(method string, args ...any) error {
	f.calls = append(f.calls, call{method, fmt.Sprint(args...)})
	if f.err != nil {
		return f.err
	}
	if f.fail {
		return errors.New("not connected")
	}
	return nil
}

func (f *fakeWA) CreateGroup(_ context.Context, name, description string, people []types.JID, parent types.JID, community bool) (wa.Group, error) {
	err := f.record("create", name, " ", description, " ", people, " parent=", parent, " community=", community)
	return wa.Group{JID: topicJID, Name: name, Kind: "group", ParentJID: parent.String()}, err
}

func (f *fakeWA) GroupInfo(_ context.Context, jid types.JID) (wa.Group, error) {
	return wa.Group{JID: jid.String(), Name: "🥗 Dieta", Kind: "group", ParentJID: communityJID}, f.record("info", jid)
}

func (f *fakeWA) UpdateParticipants(_ context.Context, jid types.JID, action string, people []types.JID) ([]wa.Participant, error) {
	err := f.record("participants", jid, " ", action, " ", people)
	return []wa.Participant{{Person: wa.Person{PN: people[0].User}, IsAdmin: action == "promote"}}, err
}

func (f *fakeWA) InviteLink(_ context.Context, jid types.JID, reset bool) (string, error) {
	return "https://chat.whatsapp.com/TEST", f.record("invite", jid, " reset=", reset)
}

func (f *fakeWA) LinkGroup(_ context.Context, parent, child types.JID) error {
	return f.record("link", parent, " ", child)
}

func (f *fakeWA) UnlinkGroup(_ context.Context, child types.JID) error {
	return f.record("unlink", child)
}

func (f *fakeWA) LeaveGroup(_ context.Context, jid types.JID) error { return f.record("leave", jid) }

func TestGroupRoutesNeedBridgeToken(t *testing.T) {
	s, wa, _ := setup(t)
	routes := [][2]string{
		{"POST", "/v1/groups"},
		{"GET", "/v1/groups/" + topicJID},
		{"POST", "/v1/groups/" + topicJID + "/participants"},
		{"GET", "/v1/groups/" + topicJID + "/invite"},
		{"POST", "/v1/groups/" + topicJID + "/link"},
		{"POST", "/v1/groups/" + topicJID + "/unlink"},
		{"POST", "/v1/groups/" + topicJID + "/leave"},
	}
	for _, rt := range routes {
		if w := do(s, rt[0], rt[1], notifyTok, `{}`); w.Code != 403 {
			t.Errorf("%s %s with notify token: got %d", rt[0], rt[1], w.Code)
		}
		if w := do(s, rt[0], rt[1], "", `{}`); w.Code != 401 {
			t.Errorf("%s %s without token: got %d", rt[0], rt[1], w.Code)
		}
	}
	if len(wa.calls) != 0 {
		t.Errorf("WhatsApp was called: %v", wa.calls)
	}
}

func TestCreateCommunityAndLinkedGroup(t *testing.T) {
	s, f, _ := setup(t)
	if w := do(s, "POST", "/v1/groups", bridgeTok, `{"name":"Assistentes","description":"Tópicos","community":true}`); w.Code != 200 {
		t.Fatalf("community: %d %s", w.Code, w.Body)
	}
	w := do(s, "POST", "/v1/groups", bridgeTok,
		`{"name":"🥗 Dieta","participants":["5571999999999"],"parent":"`+communityJID+`"}`)
	var g wa.Group
	json.Unmarshal(w.Body.Bytes(), &g)
	if w.Code != 200 || g.JID != topicJID || g.ParentJID != communityJID {
		t.Fatalf("group: %d %s", w.Code, w.Body)
	}
	want := []string{
		"create" + fmt.Sprint("Assistentes", " ", "Tópicos", " ", []types.JID{}, " parent=", types.JID{}, " community=", true),
		"create" + fmt.Sprint("🥗 Dieta", " ", "", " ", []types.JID{types.NewJID("5571999999999", types.DefaultUserServer)},
			" parent=", types.NewJID("120363000000000009", types.GroupServer), " community=", false),
	}
	for i, c := range f.calls {
		if c.method+c.args != want[i] {
			t.Errorf("call %d = %s%s\nwant %s", i, c.method, c.args, want[i])
		}
	}
}

func TestCreateGroupValidation(t *testing.T) {
	s, f, _ := setup(t)
	for _, body := range []string{
		`{"name":"  "}`,
		`{"name":"x","community":true,"parent":"` + communityJID + `"}`,
		`{"name":"x","parent":"5571999999999@s.whatsapp.net"}`,
		`{"name":"x","participants":["+55 71"]}`,
		`{"name":"x","description":"only for communities"}`,
		`{"name":"x","participants":["` + topicJID + `"]}`,
		`not json`,
	} {
		if w := do(s, "POST", "/v1/groups", bridgeTok, body); w.Code != 400 {
			t.Errorf("%s: got %d", body, w.Code)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("WhatsApp was called: %v", f.calls)
	}
}

func TestGroupInfoAndBadJID(t *testing.T) {
	s, _, _ := setup(t)
	w := do(s, "GET", "/v1/groups/"+topicJID, bridgeTok, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"parent_jid":"`+communityJID+`"`) {
		t.Errorf("info: %d %s", w.Code, w.Body)
	}
	if w := do(s, "GET", "/v1/groups/5571999999999@s.whatsapp.net", bridgeTok, ""); w.Code != 400 {
		t.Errorf("non-group JID: got %d", w.Code)
	}
}

func TestParticipants(t *testing.T) {
	s, f, _ := setup(t)
	w := do(s, "POST", "/v1/groups/"+topicJID+"/participants", bridgeTok, `{"action":"promote","participants":["5571999999999"]}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"is_admin":true`) {
		t.Errorf("promote: %d %s", w.Code, w.Body)
	}
	for _, body := range []string{
		`{"action":"remove","participants":["5571999999999"]}`,
		`{"action":"add","participants":[]}`,
		`{"action":"add","participants":["abc"]}`,
	} {
		if w := do(s, "POST", "/v1/groups/"+topicJID+"/participants", bridgeTok, body); w.Code != 400 {
			t.Errorf("%s: got %d", body, w.Code)
		}
	}
	if len(f.calls) != 1 {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestInviteLinkUnlinkLeave(t *testing.T) {
	s, f, _ := setup(t)
	if w := do(s, "GET", "/v1/groups/"+topicJID+"/invite?reset=true", bridgeTok, ""); w.Code != 200 ||
		!strings.Contains(w.Body.String(), "chat.whatsapp.com") {
		t.Errorf("invite: %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", "/v1/groups/"+topicJID+"/link", bridgeTok, `{"parent":"`+communityJID+`"}`); w.Code != 204 {
		t.Errorf("link: %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", "/v1/groups/"+topicJID+"/link", bridgeTok, `{"parent":"nope"}`); w.Code != 400 {
		t.Errorf("link bad parent: %d", w.Code)
	}
	if w := do(s, "POST", "/v1/groups/"+topicJID+"/unlink", bridgeTok, ""); w.Code != 204 {
		t.Errorf("unlink: %d", w.Code)
	}
	if w := do(s, "POST", "/v1/groups/"+topicJID+"/leave", bridgeTok, ""); w.Code != 204 {
		t.Errorf("leave: %d", w.Code)
	}
	got := []string{}
	for _, c := range f.calls {
		got = append(got, c.method)
	}
	if strings.Join(got, ",") != "invite,link,unlink,leave" {
		t.Errorf("calls = %v", got)
	}
	if !strings.Contains(f.calls[0].args, "reset=true") {
		t.Errorf("reset not passed: %s", f.calls[0].args)
	}
}

func TestGroupFailureIs502(t *testing.T) {
	s, f, _ := setup(t)
	f.fail = true
	if w := do(s, "POST", "/v1/groups/"+topicJID+"/leave", bridgeTok, ""); w.Code != 502 {
		t.Errorf("got %d", w.Code)
	}
}

func TestRateLimitIs429(t *testing.T) {
	s, f, _ := setup(t)
	f.err = fmt.Errorf("wrapped: %w", whatsmeow.ErrIQRateOverLimit)
	w := do(s, "POST", "/v1/groups", bridgeTok, `{"name":"x"}`)
	if w.Code != 429 || !strings.Contains(w.Body.String(), "rate-overlimit") {
		t.Errorf("got %d %s", w.Code, w.Body)
	}
}
