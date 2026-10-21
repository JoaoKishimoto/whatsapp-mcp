package wa

import waBinary "go.mau.fi/whatsmeow/binary"

// CommunityCreateNode is the <create> request for a community, as WhatsApp's own clients
// send it (same shape as Baileys' communityCreate). whatsmeow's CreateGroup(IsParent) adds
// regular-group settings (member_add_mode, ephemeral, membership_approval_mode) and no
// description; WhatsApp answers that with 400 bad-request (seen on 2026-10-20).
//
// No create_general_chat: it would add a "General" group, which the bridge would take for a new topic.
func CommunityCreateNode(name, description, descriptionID string) waBinary.Node {
	return waBinary.Node{
		Tag:   "create",
		Attrs: waBinary.Attrs{"subject": name},
		Content: []waBinary.Node{
			{
				Tag:     "description",
				Attrs:   waBinary.Attrs{"id": descriptionID},
				Content: []waBinary.Node{{Tag: "body", Content: []byte(description)}},
			},
			{Tag: "parent", Attrs: waBinary.Attrs{"default_membership_approval_mode": "request_required"}},
			{Tag: "allow_non_admin_sub_group_creation"},
		},
	}
}
