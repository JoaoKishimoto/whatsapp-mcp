package wa

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

var (
	phone    = types.NewJID("5571999999999", types.DefaultUserServer)
	lid      = types.NewJID("123456789", types.HiddenUserServer)
	group    = types.NewJID("120363000000000001", types.GroupServer)
	textBody = &waE2E.Message{Conversation: proto.String("oi")}
)

func event(src types.MessageSource, m *waE2E.Message) *events.Message {
	return &events.Message{Info: types.MessageInfo{MessageSource: src, ID: "3EB0AAA"}, Message: m}
}

func TestSender(t *testing.T) {
	lidOnDevice := lid
	lidOnDevice.Device = 12
	cases := []struct {
		name string
		src  types.MessageSource
		want Person
	}{
		{"dm by phone", types.MessageSource{Chat: phone, Sender: phone}, Person{PN: "5571999999999"}},
		{"dm by lid with phone alt", types.MessageSource{Chat: lid, Sender: lid, SenderAlt: phone},
			Person{PN: "5571999999999", LID: "123456789@lid"}},
		{"dm by lid only", types.MessageSource{Chat: lid, Sender: lid}, Person{LID: "123456789@lid"}},
		{"group lid sender with phone alt", types.MessageSource{Chat: group, IsGroup: true, Sender: lid, SenderAlt: phone},
			Person{PN: "5571999999999", LID: "123456789@lid"}},
		{"group phone sender with lid alt", types.MessageSource{Chat: group, IsGroup: true, Sender: phone, SenderAlt: lid},
			Person{PN: "5571999999999", LID: "123456789@lid"}},
		{"device suffix dropped", types.MessageSource{Chat: group, IsGroup: true, Sender: lidOnDevice},
			Person{LID: "123456789@lid"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SenderOf(c.src); got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestChatKind(t *testing.T) {
	for jid, want := range map[types.JID]string{
		phone: "dm", lid: "dm", group: "group",
		types.NewJID("1234", types.BroadcastServer):  "broadcast",
		types.NewJID("1234", types.NewsletterServer): "newsletter",
	} {
		msg, ok := Normalize(event(types.MessageSource{Chat: jid, Sender: phone}, textBody))
		if !ok || msg.ChatKind != want {
			t.Errorf("%s: got %q (ok=%v), want %q", jid, msg.ChatKind, ok, want)
		}
	}
}

func TestStatusIgnored(t *testing.T) {
	if _, ok := Normalize(event(types.MessageSource{Chat: types.StatusBroadcastJID, Sender: phone}, textBody)); ok {
		t.Fatal("status update should be ignored")
	}
}

func TestNothingToShowIgnored(t *testing.T) {
	reaction := &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: proto.String("👍")}}
	if _, ok := Normalize(event(types.MessageSource{Chat: phone, Sender: phone}, reaction)); ok {
		t.Fatal("reaction has no text or attachment and should be ignored")
	}
}

func TestQuote(t *testing.T) {
	m := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("e amanhã?"),
		ContextInfo: &waE2E.ContextInfo{
			StanzaID:      proto.String("3EB0QUOTED"),
			Participant:   proto.String("123456789@lid"),
			QuotedMessage: &waE2E.Message{Conversation: proto.String("Briefing de hoje")},
		},
	}}
	msg, ok := Normalize(event(types.MessageSource{Chat: phone, Sender: phone}, m))
	if !ok || msg.Text != "e amanhã?" {
		t.Fatalf("got %+v ok=%v", msg, ok)
	}
	want := Quoted{MessageID: "3EB0QUOTED", Sender: "123456789@lid", Text: "Briefing de hoje"}
	if msg.Quoted == nil || *msg.Quoted != want {
		t.Errorf("quoted = %+v, want %+v", msg.Quoted, want)
	}
}

func TestDocument(t *testing.T) {
	m := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		Mimetype: proto.String("application/pdf"), FileName: proto.String("dieta.pdf"),
		FileLength: proto.Uint64(820311), Caption: proto.String("plano novo"),
	}}
	msg, ok := Normalize(event(types.MessageSource{Chat: phone, Sender: phone}, m))
	want := Attachment{Kind: "document", MIME: "application/pdf", Filename: "dieta.pdf", Size: 820311}
	if !ok || msg.Attachment == nil || *msg.Attachment != want || msg.Text != "plano novo" {
		t.Errorf("got %+v attachment=%+v ok=%v", msg, msg.Attachment, ok)
	}
}

func TestImageWithoutCaption(t *testing.T) {
	m := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), FileLength: proto.Uint64(1000)}}
	msg, ok := Normalize(event(types.MessageSource{Chat: phone, Sender: phone}, m))
	if !ok || msg.Attachment == nil || msg.Attachment.Kind != "image" || msg.Text != "" {
		t.Errorf("got %+v attachment=%+v ok=%v", msg, msg.Attachment, ok)
	}
}

func TestVoiceNote(t *testing.T) {
	m := &waE2E.Message{AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg; codecs=opus"), PTT: proto.Bool(true), Seconds: proto.Uint32(7)}}
	msg, ok := Normalize(event(types.MessageSource{Chat: phone, Sender: phone}, m))
	if !ok || msg.Attachment == nil || msg.Attachment.Kind != "audio" || !msg.Attachment.PTT {
		t.Errorf("got %+v attachment=%+v ok=%v", msg, msg.Attachment, ok)
	}
}

func TestQuoteContext(t *testing.T) {
	ci := QuoteContext("3EB0Q", types.NewJID("5571999999999", types.DefaultUserServer), "")
	if ci.GetStanzaID() != "3EB0Q" || ci.GetParticipant() != "5571999999999@s.whatsapp.net" || ci.QuotedMessage != nil {
		t.Errorf("got %v", ci)
	}
}

func TestEdit(t *testing.T) {
	m := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
		Key:           &waCommon.MessageKey{ID: proto.String("3EB0ORIGINAL")},
		EditedMessage: &waE2E.Message{Conversation: proto.String("texto corrigido")},
	}}
	msg, ok := Normalize(event(types.MessageSource{Chat: phone, Sender: phone}, m))
	if !ok || msg.EditOf != "3EB0ORIGINAL" || msg.Text != "texto corrigido" || msg.MessageID != "3EB0AAA" {
		t.Errorf("got %+v ok=%v", msg, ok)
	}
}

func TestPushNameAndFromMe(t *testing.T) {
	e := event(types.MessageSource{Chat: phone, Sender: phone, IsFromMe: true}, textBody)
	e.Info.PushName = "João"
	msg, _ := Normalize(e)
	if msg.Sender.PushName != "João" || !msg.IsFromMe {
		t.Errorf("got %+v", msg)
	}
}

// anaCard is a vCard as the WhatsApp phone app sends it, with fields the gateway must drop.
const anaCard = "BEGIN:VCARD\r\nVERSION:3.0\r\nN:Souza;Ana;;;\r\nFN:Ana Souza\r\n" +
	"item1.TEL;waid=5511900000003:+55 11 90000-0003\r\nitem1.X-ABLabel:Celular\r\n" +
	"TEL;type=CELL;type=VOICE;waid=5511900000003:+55 11 90000-0003\r\n" +
	"EMAIL;type=INTERNET:ana@example.com\r\nADR;type=HOME:;;Rua Exemplo 1;São Paulo;;;\r\n" +
	"NOTE:senha do portão 4321\r\nORG:Exemplo Ltda\r\nEND:VCARD"

func TestContactCard(t *testing.T) {
	m := &waE2E.Message{ContactMessage: &waE2E.ContactMessage{DisplayName: proto.String("Ana Souza"), Vcard: proto.String(anaCard)}}
	msg, ok := Normalize(event(types.MessageSource{Chat: phone, Sender: phone}, m))
	want := []Contact{{Name: "Ana Souza", Phones: []string{"5511900000003"}}}
	if !ok || !reflect.DeepEqual(msg.Contacts, want) || msg.Text != "" || msg.Attachment != nil {
		t.Errorf("got %+v ok=%v", msg, ok)
	}
}

func TestContactsArray(t *testing.T) {
	var cards []*waE2E.ContactMessage
	for i := 1; i <= 12; i++ {
		n := fmt.Sprintf("55119000000%02d", i)
		cards = append(cards, &waE2E.ContactMessage{
			DisplayName: proto.String("Contato " + n[len(n)-2:]),
			Vcard:       proto.String("BEGIN:VCARD\nFN:x\nTEL;waid=" + n + ":+" + n + "\nEND:VCARD"),
		})
	}
	m := &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{DisplayName: proto.String("12 contatos"), Contacts: cards}}
	msg, ok := Normalize(event(types.MessageSource{Chat: phone, Sender: phone}, m))
	if !ok || len(msg.Contacts) != maxContacts {
		t.Fatalf("got %d contacts ok=%v, want %d", len(msg.Contacts), ok, maxContacts)
	}
	first, last := msg.Contacts[0], msg.Contacts[maxContacts-1]
	if first.Name != "Contato 01" || first.Phones[0] != "5511900000001" || last.Phones[0] != "5511900000010" {
		t.Errorf("got first %+v, last %+v", first, last)
	}
}

func TestVCardPhones(t *testing.T) {
	cases := []struct {
		name, vcard string
		want        []string
	}{
		{"waid preferred over written number", "TEL;type=CELL;waid=5511900000003:+55 11 90000-0099", []string{"5511900000003"}},
		{"written number without waid", "TEL;TYPE=cell:+55 (11) 90000-0004", []string{"5511900000004"}},
		{"deduplicated, card order", "TEL;waid=5511900000005:+55 11 90000-0005\nTEL:+55 11 90000-0006\nitem2.TEL:+55 11 90000-0005",
			[]string{"5511900000005", "5511900000006"}},
		{"no TEL", "FN:Ana Souza\nEMAIL:ana@example.com", []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, got := parseVCard("BEGIN:VCARD\r\n" + strings.ReplaceAll(c.vcard, "\n", "\r\n") + "\r\nEND:VCARD"); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestContactName(t *testing.T) {
	// No displayName: the vCard FN (folded and escaped here) names the contact.
	card := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Souza\\, \r\n Ana\r\nEND:VCARD"
	m := &waE2E.Message{ContactMessage: &waE2E.ContactMessage{Vcard: proto.String(card)}}
	msg, ok := Normalize(event(types.MessageSource{Chat: phone, Sender: phone}, m))
	if !ok || len(msg.Contacts) != 1 || msg.Contacts[0].Name != "Souza, Ana" {
		t.Errorf("got %+v ok=%v", msg.Contacts, ok)
	}
}

func TestContactWithoutPhones(t *testing.T) {
	// A card without TEL is kept, phones empty; one with neither name nor TEL is dropped.
	m := &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{Contacts: []*waE2E.ContactMessage{
		{DisplayName: proto.String("Ana Souza"), Vcard: proto.String("BEGIN:VCARD\nFN:Ana Souza\nEMAIL:ana@example.com\nEND:VCARD")},
		{Vcard: proto.String("BEGIN:VCARD\nEMAIL:x@example.com\nEND:VCARD")},
	}}}
	msg, ok := Normalize(event(types.MessageSource{Chat: phone, Sender: phone}, m))
	if !ok || len(msg.Contacts) != 1 {
		t.Fatalf("got %+v ok=%v", msg.Contacts, ok)
	}
	b, _ := json.Marshal(msg.Contacts)
	if string(b) != `[{"name":"Ana Souza","phones":[]}]` {
		t.Errorf("got %s", b)
	}
	empty := &waE2E.Message{ContactMessage: &waE2E.ContactMessage{Vcard: proto.String("BEGIN:VCARD\nEND:VCARD")}}
	if _, ok := Normalize(event(types.MessageSource{Chat: phone, Sender: phone}, empty)); ok {
		t.Error("an empty card has nothing to show and should be ignored")
	}
}

func TestContactJSONHasNoOtherFields(t *testing.T) {
	m := &waE2E.Message{ContactMessage: &waE2E.ContactMessage{DisplayName: proto.String("Ana Souza"), Vcard: proto.String(anaCard)}}
	msg, _ := Normalize(event(types.MessageSource{Chat: phone, Sender: phone}, m))
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"example.com", "Rua Exemplo", "4321", "Exemplo Ltda", "Celular", "VCARD", "90000-0003"} {
		if strings.Contains(string(b), leak) {
			t.Errorf("event JSON leaks %q: %s", leak, b)
		}
	}
	if !strings.Contains(string(b), `"contacts":[{"name":"Ana Souza","phones":["5511900000003"]}]`) {
		t.Errorf("got %s", b)
	}
	plain, _ := Normalize(event(types.MessageSource{Chat: phone, Sender: phone}, textBody))
	if b, _ := json.Marshal(plain); strings.Contains(string(b), "contacts") {
		t.Errorf("contacts should be omitted when empty: %s", b)
	}
}

func TestContactQuoted(t *testing.T) {
	m := &waE2E.Message{ContactMessage: &waE2E.ContactMessage{
		DisplayName: proto.String("Ana Souza"), Vcard: proto.String(anaCard),
		ContextInfo: &waE2E.ContextInfo{
			StanzaID:      proto.String("3EB0QUOTED"),
			Participant:   proto.String("123456789@lid"),
			QuotedMessage: &waE2E.Message{Conversation: proto.String("me passa o contato dela")},
		},
	}}
	msg, ok := Normalize(event(types.MessageSource{Chat: phone, Sender: phone}, m))
	want := Quoted{MessageID: "3EB0QUOTED", Sender: "123456789@lid", Text: "me passa o contato dela"}
	if !ok || msg.Quoted == nil || *msg.Quoted != want || len(msg.Contacts) != 1 {
		t.Errorf("got %+v quoted=%+v ok=%v", msg, msg.Quoted, ok)
	}
}
