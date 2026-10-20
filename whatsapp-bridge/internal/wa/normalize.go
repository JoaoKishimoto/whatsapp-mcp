// Package wa turns whatsmeow events into the gateway's own event shape (PLANO.md §8).
package wa

import (
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// Person identifies a WhatsApp user by phone number and/or LID.
// Either may be empty: WhatsApp does not always send both.
type Person struct {
	PN       string `json:"pn,omitempty"`  // phone digits, e.g. "5571999999999"
	LID      string `json:"lid,omitempty"` // e.g. "1234@lid"
	PushName string `json:"push_name,omitempty"`
}

type Quoted struct {
	MessageID string `json:"message_id"`
	Sender    string `json:"sender,omitempty"` // JID as WhatsApp sends it (phone or LID)
	Text      string `json:"text,omitempty"`
}

type Attachment struct {
	Kind     string `json:"kind"` // image | video | audio | document | sticker
	MIME     string `json:"mime,omitempty"`
	Filename string `json:"filename,omitempty"`
	Size     uint64 `json:"size,omitempty"`
}

type Message struct {
	MessageID  string      `json:"message_id"`
	EditOf     string      `json:"edit_of,omitempty"` // set when this is an edit of an earlier message
	Timestamp  time.Time   `json:"timestamp"`
	ChatJID    string      `json:"chat_jid"`
	ChatKind   string      `json:"chat_kind"` // dm | group | broadcast | newsletter
	Sender     Person      `json:"sender"`
	IsFromMe   bool        `json:"is_from_me"`
	Text       string      `json:"text,omitempty"`
	Quoted     *Quoted     `json:"quoted,omitempty"`
	Attachment *Attachment `json:"attachment,omitempty"`
}

// Normalize converts a whatsmeow message. ok is false for messages the gateway
// never forwards (status updates) and for messages with nothing to show
// (reactions, receipts of protocol actions, unknown types).
// Community vs. plain group needs GroupInfo, so the caller refines ChatKind.
func Normalize(evt *events.Message) (msg Message, ok bool) {
	info := evt.Info
	if info.Chat == types.StatusBroadcastJID {
		return Message{}, false
	}
	content := evt.Message
	msg = Message{
		MessageID: info.ID,
		Timestamp: info.Timestamp,
		ChatJID:   info.Chat.String(),
		ChatKind:  chatKind(info.Chat),
		Sender:    SenderOf(info.MessageSource),
		IsFromMe:  info.IsFromMe,
	}
	msg.Sender.PushName = info.PushName

	if p := content.GetProtocolMessage(); p.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT {
		msg.EditOf = p.GetKey().GetID()
		content = p.GetEditedMessage()
	}

	msg.Text = textOf(content)
	msg.Attachment = attachmentOf(content)
	if ctx := contextInfoOf(content); ctx.GetStanzaID() != "" {
		msg.Quoted = &Quoted{
			MessageID: ctx.GetStanzaID(),
			Sender:    ctx.GetParticipant(),
			Text:      textOf(ctx.GetQuotedMessage()),
		}
	}
	if msg.Text == "" && msg.Attachment == nil {
		return Message{}, false
	}
	return msg, true
}

// SenderOf fills phone and LID from whichever of Sender/SenderAlt carries each.
// The device suffix (":12") is dropped so phone, Web and Desktop all match.
func SenderOf(src types.MessageSource) Person {
	var p Person
	for _, jid := range []types.JID{src.Sender, src.SenderAlt} {
		switch jid.Server {
		case types.DefaultUserServer:
			if p.PN == "" {
				p.PN = jid.User
			}
		case types.HiddenUserServer:
			if p.LID == "" {
				p.LID = jid.ToNonAD().String()
			}
		}
	}
	return p
}

func chatKind(chat types.JID) string {
	switch chat.Server {
	case types.GroupServer:
		return "group"
	case types.BroadcastServer:
		return "broadcast"
	case types.NewsletterServer:
		return "newsletter"
	default:
		return "dm"
	}
}

func textOf(m *waE2E.Message) string {
	switch {
	case m.GetConversation() != "":
		return m.GetConversation()
	case m.GetExtendedTextMessage() != nil:
		return m.GetExtendedTextMessage().GetText()
	case m.GetImageMessage() != nil:
		return m.GetImageMessage().GetCaption()
	case m.GetVideoMessage() != nil:
		return m.GetVideoMessage().GetCaption()
	case m.GetDocumentMessage() != nil:
		return m.GetDocumentMessage().GetCaption()
	}
	return ""
}

func attachmentOf(m *waE2E.Message) *Attachment {
	switch {
	case m.GetImageMessage() != nil:
		x := m.GetImageMessage()
		return &Attachment{Kind: "image", MIME: x.GetMimetype(), Size: x.GetFileLength()}
	case m.GetVideoMessage() != nil:
		x := m.GetVideoMessage()
		return &Attachment{Kind: "video", MIME: x.GetMimetype(), Size: x.GetFileLength()}
	case m.GetAudioMessage() != nil:
		x := m.GetAudioMessage()
		return &Attachment{Kind: "audio", MIME: x.GetMimetype(), Size: x.GetFileLength()}
	case m.GetDocumentMessage() != nil:
		x := m.GetDocumentMessage()
		return &Attachment{Kind: "document", MIME: x.GetMimetype(), Filename: x.GetFileName(), Size: x.GetFileLength()}
	case m.GetStickerMessage() != nil:
		x := m.GetStickerMessage()
		return &Attachment{Kind: "sticker", MIME: x.GetMimetype(), Size: x.GetFileLength()}
	}
	return nil
}

func contextInfoOf(m *waE2E.Message) *waE2E.ContextInfo {
	switch {
	case m.GetExtendedTextMessage() != nil:
		return m.GetExtendedTextMessage().GetContextInfo()
	case m.GetImageMessage() != nil:
		return m.GetImageMessage().GetContextInfo()
	case m.GetVideoMessage() != nil:
		return m.GetVideoMessage().GetContextInfo()
	case m.GetAudioMessage() != nil:
		return m.GetAudioMessage().GetContextInfo()
	case m.GetDocumentMessage() != nil:
		return m.GetDocumentMessage().GetContextInfo()
	case m.GetStickerMessage() != nil:
		return m.GetStickerMessage().GetContextInfo()
	}
	return nil
}
