package wa

import (
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestRegistered(t *testing.T) {
	pn := types.NewJID("551190000001", types.DefaultUserServer) // registered without the extra 9
	lid := types.NewJID("100000000000001", types.HiddenUserServer)
	cases := []struct {
		name  string
		resps []types.IsOnWhatsAppResponse
		want  types.JID
		ok    bool
	}{
		{"no answer", nil, types.JID{}, false},
		{"not on WhatsApp", []types.IsOnWhatsAppResponse{{Query: "+5511900000001", JID: types.NewJID("5511900000001", types.DefaultUserServer)}}, types.JID{}, false},
		{"phone JID only", []types.IsOnWhatsAppResponse{{IsIn: true, JID: pn}}, pn, true},
		{"LID with phone number", []types.IsOnWhatsAppResponse{{IsIn: true, JID: lid, PhoneNumber: pn}}, pn, true},
		{"LID alone", []types.IsOnWhatsAppResponse{{IsIn: true, JID: lid}}, lid, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Registered(c.resps)
			if got != c.want || ok != c.ok {
				t.Errorf("got %v %v, want %v %v", got, ok, c.want, c.ok)
			}
		})
	}
}
