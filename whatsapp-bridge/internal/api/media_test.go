package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/types"

	"whatsapp-client/internal/media"
)

func (f *fakeWA) DownloadMedia(_ context.Context, chat types.JID, id string) (Downloaded, error) {
	if err := f.record("download", chat, " ", id); err != nil {
		return Downloaded{}, err
	}
	return Downloaded{Path: "/media/x/" + id + ".pdf", Kind: "document", Size: 3}, nil
}

func (f *fakeWA) SendFile(_ context.Context, to types.JID, path string, o media.Outgoing, reply *Reply) (string, error) {
	quoted := ""
	if reply != nil {
		quoted = " reply=" + reply.MessageID + "/" + reply.Sender.String()
	}
	return "3EB0FILE", f.record("file", to, " ", filepath.Base(path), " ", o.MIME, " ", o.Filename, " ptt=", o.PTT, quoted)
}

// mediaFile writes a file into the server's media directory and returns its relative path.
func mediaFile(t *testing.T, s *Server, name, content string) string {
	t.Helper()
	p := filepath.Join(s.cfg.MediaDir, "out", name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return "out/" + name
}

func TestMediaRoutesNeedBridgeToken(t *testing.T) {
	s, f, _ := setup(t)
	for _, path := range []string{"/v1/media/download", "/v1/messages/file"} {
		if w := do(s, "POST", path, notifyTok, `{}`); w.Code != 403 {
			t.Errorf("%s with notify token: got %d", path, w.Code)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("WhatsApp was called: %v", f.calls)
	}
}

func TestDownloadMedia(t *testing.T) {
	s, f, _ := setup(t)
	w := do(s, "POST", "/v1/media/download", bridgeTok, `{"chat":"123456789@lid","message_id":"3EB0DOC"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"path":"/media/x/3EB0DOC.pdf"`) {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	if f.calls[0].args != "123456789@lid 3EB0DOC" {
		t.Errorf("args = %q", f.calls[0].args)
	}
	for err, want := range map[error]int{ErrMediaUnknown: 404, ErrMediaGone: 410, ErrMediaTooBig: 413} {
		f.err = err
		if w := do(s, "POST", "/v1/media/download", bridgeTok, `{"chat":"5571999999999","message_id":"3EB0"}`); w.Code != want {
			t.Errorf("%v: got %d, want %d", err, w.Code, want)
		}
	}
	f.err = nil
	if w := do(s, "POST", "/v1/media/download", bridgeTok, `{"chat":"5571999999999"}`); w.Code != 400 {
		t.Errorf("missing message_id: got %d", w.Code)
	}
}

func TestSendFile(t *testing.T) {
	s, f, _ := setup(t)
	pdf := mediaFile(t, s, "plano.pdf", "%PDF-1.7 ...")
	body := `{"to":"5571999999999","path":"` + pdf + `","caption":"aqui"}`
	if w := do(s, "POST", "/v1/messages/file", bridgeTok, body); w.Code != 200 || !strings.Contains(w.Body.String(), "3EB0FILE") {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	if got := f.calls[0].args; got != "5571999999999@s.whatsapp.net plano.pdf application/pdf plano.pdf ptt=false" {
		t.Errorf("args = %q (MIME and file name should be filled in)", got)
	}
	voice := mediaFile(t, s, "resposta.ogg", "OggS...")
	body = `{"to":"` + topicJID + `","path":"` + voice + `","ptt":true,"reply_to":{"message_id":"3EB0Q","sender":"123456789@lid"}}`
	if w := do(s, "POST", "/v1/messages/file", bridgeTok, body); w.Code != 200 {
		t.Fatalf("voice: got %d %s", w.Code, w.Body)
	}
	if got := f.calls[1].args; !strings.HasSuffix(got, "audio/ogg resposta.ogg ptt=true reply=3EB0Q/123456789@lid") {
		t.Errorf("voice args = %q", got)
	}
}

func TestSendFileRefusals(t *testing.T) {
	s, f, _ := setup(t)
	pdf := mediaFile(t, s, "plano.pdf", "%PDF-1.7")
	outside := filepath.Join(t.TempDir(), "whatsapp.db")
	os.WriteFile(outside, []byte("session keys"), 0o600)
	big := filepath.Join(s.cfg.MediaDir, "out", "big.bin")
	if fh, err := os.Create(big); err == nil {
		fh.Truncate(MaxFileBytes + 1)
		fh.Close()
	}
	cases := []struct {
		body string
		want int
	}{
		{`{"to":"5571999999999","path":"` + outside + `"}`, 400},
		{`{"to":"5571999999999","path":"../../etc/passwd"}`, 400},
		{`{"to":"5571999999999","path":"out/missing.pdf"}`, 400},
		{`{"to":"5571999999999","path":"` + pdf + `","ptt":true}`, 400}, // a PDF is not a voice note
		{`{"to":"","path":"` + pdf + `"}`, 400},
		{`{"to":"` + topicJID + `","path":"` + pdf + `","reply_to":{"message_id":"3EB0Q"}}`, 400}, // group needs a sender
		{`{"to":"5571999999999","path":"out/big.bin"}`, 413},
	}
	for _, c := range cases {
		if w := do(s, "POST", "/v1/messages/file", bridgeTok, c.body); w.Code != c.want {
			t.Errorf("%s: got %d, want %d (%s)", c.body, w.Code, c.want, w.Body)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("WhatsApp was called: %v", f.calls)
	}
}

func TestSendTextReply(t *testing.T) {
	s, f, _ := setup(t)
	// DM: the quoted message's sender defaults to the other person.
	do(s, "POST", "/v1/messages", bridgeTok, `{"to":"5571999999999","text":"ok","reply_to":{"message_id":"3EB0A","text":"oi"}}`)
	// The bot quoting itself: no sender, the adapter uses its own JID.
	do(s, "POST", "/v1/messages", notifyTok, `{"text":"lembrete","reply_to":{"message_id":"3EB0B","from_me":true}}`)
	if len(f.sent) != 2 {
		t.Fatalf("sent = %+v", f.sent)
	}
	if r := f.sent[0].reply; r == nil || r.MessageID != "3EB0A" || r.Sender.String() != "5571999999999@s.whatsapp.net" || r.Text != "oi" {
		t.Errorf("DM reply = %+v", r)
	}
	if r := f.sent[1].reply; r == nil || !r.Sender.IsEmpty() || f.sent[1].to != owner {
		t.Errorf("own reply = %+v to %s", r, f.sent[1].to)
	}
	if w := do(s, "POST", "/v1/messages", bridgeTok, `{"to":"`+topicJID+`","text":"x","reply_to":{"message_id":"3EB0C"}}`); w.Code != 400 {
		t.Errorf("group reply without sender: got %d", w.Code)
	}
}

func TestReplyContext(t *testing.T) {
	own := types.NewJID("5500000000000", types.DefaultUserServer)
	if (*Reply)(nil).Context(own) != nil {
		t.Error("no reply should give no quote")
	}
	ci := (&Reply{MessageID: "3EB0B"}).Context(own)
	if ci.GetStanzaID() != "3EB0B" || ci.GetParticipant() != own.String() || ci.QuotedMessage != nil {
		t.Errorf("own quote = %v", ci)
	}
	lid, _ := types.ParseJID("123456789:12@lid")
	ci = (&Reply{MessageID: "3EB0C", Sender: lid, Text: "oi"}).Context(own)
	if ci.GetParticipant() != "123456789@lid" || ci.GetQuotedMessage().GetConversation() != "oi" {
		t.Errorf("quote = %v", ci)
	}
}
