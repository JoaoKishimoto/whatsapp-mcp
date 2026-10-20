package wa

import (
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
