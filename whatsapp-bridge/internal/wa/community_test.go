package wa

import "testing"

func TestCommunityCreateNode(t *testing.T) {
	n := CommunityCreateNode("Assistentes", "Tópicos do João", "3EB0ABCDEF12")
	if n.Tag != "create" || n.Attrs["subject"] != "Assistentes" {
		t.Fatalf("got %s %v", n.Tag, n.Attrs)
	}
	children := map[string]bool{}
	for _, c := range n.GetChildren() {
		children[c.Tag] = true
	}
	for _, want := range []string{"description", "parent", "allow_non_admin_sub_group_creation"} {
		if !children[want] {
			t.Errorf("missing <%s>", want)
		}
	}
	// The regular-group settings whatsmeow adds are what WhatsApp rejects for a community.
	for _, bad := range []string{"member_add_mode", "ephemeral", "membership_approval_mode", "create_general_chat"} {
		if children[bad] {
			t.Errorf("must not send <%s>", bad)
		}
	}
	desc := n.GetChildByTag("description")
	if desc.Attrs["id"] != "3EB0ABCDEF12" || string(desc.GetChildByTag("body").Content.([]byte)) != "Tópicos do João" {
		t.Errorf("description = %v", desc)
	}
	if n.GetChildByTag("parent").Attrs["default_membership_approval_mode"] != "request_required" {
		t.Error("parent approval mode missing")
	}
}
