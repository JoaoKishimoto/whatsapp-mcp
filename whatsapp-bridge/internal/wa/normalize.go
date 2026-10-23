// Package wa turns whatsmeow events into the gateway's own event shape (PLANO.md §8).
package wa

import (
	"slices"
	"strings"
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
	PTT      bool   `json:"ptt,omitempty"` // a voice note (audio recorded in the chat)
}

// Contact is a shared contact card: its name and phone numbers, nothing else from the vCard.
type Contact struct {
	Name   string   `json:"name"`
	Phones []string `json:"phones"` // digits, e.g. "5571999999999"; empty (never null) when the card has none
}

type Message struct {
	MessageID string    `json:"message_id"`
	EditOf    string    `json:"edit_of,omitempty"` // set when this is an edit of an earlier message
	Timestamp time.Time `json:"timestamp"`
	ChatJID   string    `json:"chat_jid"`
	ChatKind  string    `json:"chat_kind"` // dm | group | announcement | community | broadcast | newsletter
	// GroupParentJID is the community a group belongs to. Set, with ChatKind, by ApplyGroup.
	GroupParentJID string      `json:"group_parent_jid,omitempty"`
	Sender         Person      `json:"sender"`
	IsFromMe       bool        `json:"is_from_me"`
	Text           string      `json:"text,omitempty"`
	Quoted         *Quoted     `json:"quoted,omitempty"`
	Attachment     *Attachment `json:"attachment,omitempty"`
	Contacts       []Contact   `json:"contacts,omitempty"`
}

// Normalize converts a whatsmeow message. ok is false for messages the gateway
// never forwards (status updates) and for messages with nothing to show
// (reactions, receipts of protocol actions, unknown types).
// Community vs. plain group needs GroupInfo, so the caller refines ChatKind with ApplyGroup.
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
	msg.Contacts = contactsOf(content)
	if ctx := contextInfoOf(content); ctx.GetStanzaID() != "" {
		msg.Quoted = &Quoted{
			MessageID: ctx.GetStanzaID(),
			Sender:    ctx.GetParticipant(),
			Text:      textOf(ctx.GetQuotedMessage()),
		}
	}
	if msg.Text == "" && msg.Attachment == nil && len(msg.Contacts) == 0 {
		return Message{}, false
	}
	return msg, true
}

// SenderOf fills phone and LID from whichever of Sender/SenderAlt carries each.
// The device suffix (":12") is dropped so phone, Web and Desktop all match.
func SenderOf(src types.MessageSource) Person {
	return personOf(src.Sender, src.SenderAlt)
}

// personOf takes the first phone JID and the first LID among jids.
func personOf(jids ...types.JID) Person {
	var p Person
	for _, jid := range jids {
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
		return &Attachment{Kind: "audio", MIME: x.GetMimetype(), Size: x.GetFileLength(), PTT: x.GetPTT()}
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
	case m.GetContactMessage() != nil:
		return m.GetContactMessage().GetContextInfo()
	case m.GetContactsArrayMessage() != nil:
		return m.GetContactsArrayMessage().GetContextInfo()
	}
	return nil
}

// maxContacts caps a contactsArrayMessage; the phone lets you share many more at once.
const maxContacts = 10

func contactsOf(m *waE2E.Message) []Contact {
	cards := m.GetContactsArrayMessage().GetContacts()
	if c := m.GetContactMessage(); c != nil {
		cards = []*waE2E.ContactMessage{c}
	}
	var out []Contact
	for _, card := range cards {
		if len(out) == maxContacts {
			break
		}
		fn, phones := parseVCard(card.GetVcard())
		name := strings.TrimSpace(card.GetDisplayName())
		if name == "" {
			name = fn
		}
		if name != "" || len(phones) > 0 {
			out = append(out, Contact{Name: name, Phones: phones})
		}
	}
	return out
}

var (
	vcardUnfolder  = strings.NewReplacer("\r\n ", "", "\r\n\t", "", "\n ", "", "\n\t", "")
	vcardUnescaper = strings.NewReplacer(`\,`, ",", `\;`, ";", `\\`, `\`, `\n`, " ", `\N`, " ")
)

// parseVCard reads FN and the TEL numbers off a vCard and ignores every other field.
// A TEL's waid= parameter (the WhatsApp account, put there by WhatsApp) wins over the
// number as written. Phones are digits only, deduplicated, in card order.
func parseVCard(vcard string) (fn string, phones []string) {
	phones = []string{}
	for _, line := range strings.Split(vcardUnfolder.Replace(vcard), "\n") {
		head, value, ok := strings.Cut(strings.TrimRight(line, "\r"), ":")
		if !ok {
			continue
		}
		params := strings.Split(head, ";")
		prop := strings.ToUpper(params[0])
		if i := strings.LastIndexByte(prop, '.'); i >= 0 {
			prop = prop[i+1:] // "item1.TEL" is TEL
		}
		switch prop {
		case "FN":
			if fn == "" {
				fn = strings.TrimSpace(vcardUnescaper.Replace(value))
			}
		case "TEL":
			number := digitsOf(value)
			for _, p := range params[1:] {
				if k, v, _ := strings.Cut(p, "="); strings.EqualFold(k, "waid") && digitsOf(v) != "" {
					number = digitsOf(v)
				}
			}
			if number != "" && !slices.Contains(phones, number) {
				phones = append(phones, number)
			}
		}
	}
	return fn, phones
}

func digitsOf(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
