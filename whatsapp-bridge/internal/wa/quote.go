package wa

import (
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// QuoteContext makes an outgoing message a reply to messageID, written by sender.
// text, when known, is what the quote preview shows; without it the phone looks
// the quoted message up by id.
func QuoteContext(messageID string, sender types.JID, text string) *waE2E.ContextInfo {
	ci := &waE2E.ContextInfo{
		StanzaID:    proto.String(messageID),
		Participant: proto.String(sender.ToNonAD().String()),
	}
	if text != "" {
		ci.QuotedMessage = &waE2E.Message{Conversation: proto.String(text)}
	}
	return ci
}
