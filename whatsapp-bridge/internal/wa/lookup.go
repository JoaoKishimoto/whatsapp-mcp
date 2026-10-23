package wa

import "go.mau.fi/whatsmeow/types"

// Registered reads the answer to an IsOnWhatsApp query for one number: the JID WhatsApp
// has for it, or false when the number has no account. The JID can differ from the digits
// asked: in Brazil an old account may be registered without the 9 after the area code.
// WhatsApp answers in LID addressing mode, so JID may be the LID and PhoneNumber the
// number; the phone-number JID is preferred, the LID is the fallback.
func Registered(resps []types.IsOnWhatsAppResponse) (types.JID, bool) {
	for _, r := range resps {
		if !r.IsIn {
			continue
		}
		if r.PhoneNumber.Server == types.DefaultUserServer && r.PhoneNumber.User != "" {
			return r.PhoneNumber, true
		}
		if !r.JID.IsEmpty() {
			return r.JID, true
		}
	}
	return types.JID{}, false
}
