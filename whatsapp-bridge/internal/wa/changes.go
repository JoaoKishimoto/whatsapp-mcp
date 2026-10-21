package wa

import (
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// GroupChange is the group_changed event (PLANO.md §8): a group the bot is in was renamed
// or deleted, people joined, left or changed role, or it was linked to or unlinked from a
// community. Description, settings and invite-link changes are not forwarded.
type GroupChange struct {
	JID       string     `json:"jid"`
	By        *Person    `json:"by,omitempty"`
	Name      string     `json:"name,omitempty"` // the new name
	Deleted   bool       `json:"deleted,omitempty"`
	Joined    []Person   `json:"joined,omitempty"`
	Left      []Person   `json:"left,omitempty"`
	Promoted  []Person   `json:"promoted,omitempty"`
	Demoted   []Person   `json:"demoted,omitempty"`
	Linked    *GroupLink `json:"linked,omitempty"`
	Unlinked  *GroupLink `json:"unlinked,omitempty"`
	Timestamp time.Time  `json:"timestamp"`
}

type GroupLink struct {
	JID    string `json:"jid"`
	Name   string `json:"name,omitempty"`
	Type   string `json:"type"` // parent_group | sub_group | sibling_group
	Reason string `json:"reason,omitempty"`
}

// Changed converts whatsmeow's group update; ok is false when nothing above changed.
func Changed(evt *events.GroupInfo) (c GroupChange, ok bool) {
	c = GroupChange{
		JID: evt.JID.String(), Deleted: evt.Delete != nil, Timestamp: evt.Timestamp,
		Joined: people(evt.Join), Left: people(evt.Leave), Promoted: people(evt.Promote), Demoted: people(evt.Demote),
		Linked: link(evt.Link), Unlinked: link(evt.Unlink),
	}
	if evt.Name != nil {
		c.Name = evt.Name.Name
	}
	if evt.Sender != nil {
		by := []types.JID{*evt.Sender}
		if evt.SenderPN != nil {
			by = append(by, *evt.SenderPN)
		}
		p := personOf(by...)
		c.By = &p
	}
	ok = evt.Name != nil || c.Deleted || c.Joined != nil || c.Left != nil ||
		c.Promoted != nil || c.Demoted != nil || c.Linked != nil || c.Unlinked != nil
	return c, ok
}

func people(jids []types.JID) []Person {
	if len(jids) == 0 {
		return nil
	}
	out := make([]Person, 0, len(jids))
	for _, j := range jids {
		out = append(out, personOf(j))
	}
	return out
}

func link(l *types.GroupLinkChange) *GroupLink {
	if l == nil {
		return nil
	}
	return &GroupLink{JID: l.Group.JID.String(), Name: l.Group.Name, Type: string(l.Type), Reason: string(l.UnlinkReason)}
}

// Call is the call event: someone is calling the bot. The gateway cannot answer
// (whatsmeow has no call audio); the bridge may reject it with FromJID and CallID.
type Call struct {
	CallID    string    `json:"call_id"`
	From      Person    `json:"from"`
	FromJID   string    `json:"from_jid"` // for POST /v1/calls/reject
	Video     bool      `json:"video"`
	GroupJID  string    `json:"group_jid,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

func CallOf(evt *events.CallOffer) Call {
	creator := evt.CallCreator
	if creator.IsEmpty() {
		creator = evt.From
	}
	c := Call{
		CallID: evt.CallID, From: personOf(evt.CallCreator, evt.CallCreatorAlt, evt.From),
		FromJID: creator.ToNonAD().String(), Timestamp: evt.Timestamp,
	}
	if !evt.GroupJID.IsEmpty() {
		c.GroupJID = evt.GroupJID.String()
	}
	if evt.Data != nil {
		_, c.Video = evt.Data.GetOptionalChildByTag("video")
	}
	return c
}
