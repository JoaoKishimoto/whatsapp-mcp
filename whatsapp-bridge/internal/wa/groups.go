package wa

import (
	"errors"
	"strings"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// Group is the gateway's view of a group, used by the API and the group_joined event.
type Group struct {
	JID          string        `json:"jid"`
	Name         string        `json:"name"`
	Kind         string        `json:"kind"`                 // community | announcement | group
	ParentJID    string        `json:"parent_jid,omitempty"` // community this group is linked to
	Participants []Participant `json:"participants,omitempty"`
}

type Participant struct {
	Person
	IsAdmin bool `json:"is_admin"`
	// Error is WhatsApp's code when adding this person failed (e.g. 403: their privacy
	// settings block it; send an invite link instead). 0 means OK.
	Error int `json:"error,omitempty"`
}

// GroupJoined is the group_joined event: the bot was added to (or created) a group.
type GroupJoined struct {
	Group
	AddedBy *Person `json:"added_by,omitempty"`
}

// GroupFrom converts whatsmeow's group info.
// A community is a parent group; its announcement channel is the default subgroup.
func GroupFrom(info *types.GroupInfo) Group {
	g := Group{JID: info.JID.String(), Name: info.Name, Kind: "group", Participants: Participants(info.Participants)}
	switch {
	case info.IsParent:
		g.Kind = "community"
	case info.IsDefaultSubGroup:
		g.Kind = "announcement"
	}
	if !info.LinkedParentJID.IsEmpty() {
		g.ParentJID = info.LinkedParentJID.String()
	}
	return g
}

func Participants(ps []types.GroupParticipant) []Participant {
	out := make([]Participant, 0, len(ps))
	for _, p := range ps {
		out = append(out, Participant{
			Person:  personOf(p.PhoneNumber, p.LID, p.JID),
			IsAdmin: p.IsAdmin || p.IsSuperAdmin,
			Error:   p.Error,
		})
	}
	return out
}

func Joined(evt *events.JoinedGroup) GroupJoined {
	j := GroupJoined{Group: GroupFrom(&evt.GroupInfo)}
	var by []types.JID
	for _, jid := range []*types.JID{evt.SenderPN, evt.Sender} {
		if jid != nil {
			by = append(by, *jid)
		}
	}
	if p := personOf(by...); p != (Person{}) {
		j.AddedBy = &p
	}
	return j
}

// ApplyGroup sets what Normalize cannot know without group info.
func ApplyGroup(msg *Message, g Group) {
	msg.ChatKind = g.Kind
	msg.GroupParentJID = g.ParentJID
}

// ParseRecipient accepts phone digits ("5571999999999") or a full JID ("…@g.us", "…@lid").
func ParseRecipient(s string) (types.JID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return types.JID{}, errors.New("empty recipient")
	}
	if !strings.Contains(s, "@") {
		for _, r := range s {
			if r < '0' || r > '9' {
				return types.JID{}, errors.New("a phone number must be digits only, with country code")
			}
		}
		return types.NewJID(s, types.DefaultUserServer), nil
	}
	return types.ParseJID(s)
}

// ParseGroupJID accepts only group JIDs ("…@g.us").
func ParseGroupJID(s string) (types.JID, error) {
	jid, err := types.ParseJID(s)
	if err != nil || jid.Server != types.GroupServer || jid.User == "" {
		return types.JID{}, errors.New("not a group JID (…@g.us)")
	}
	return jid, nil
}
