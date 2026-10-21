package media

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestStoreKeepsTheProtoNotTheURL(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	doc := &waE2E.DocumentMessage{
		DirectPath: proto.String("/v/t62.7119-24/123.enc?ccb=11-4&oh=sig&oe=exp"), // the query is part of it
		MediaKey:   []byte("key"), FileSHA256: []byte("sha"), FileEncSHA256: []byte("enc"),
		Mimetype: proto.String("application/pdf"), FileName: proto.String("dieta.pdf"), FileLength: proto.Uint64(820311),
	}
	if ok, err := s.Save("5571999999999@s.whatsapp.net", "3EB0DOC", &waE2E.Message{DocumentMessage: doc}); !ok || err != nil {
		t.Fatalf("save: %v %v", ok, err)
	}
	if ok, _ := s.Save("5571999999999@s.whatsapp.net", "3EB0TXT", &waE2E.Message{Conversation: proto.String("oi")}); ok {
		t.Error("a text message has no attachment to keep")
	}
	it, ok, err := s.Get("5571999999999@s.whatsapp.net", "3EB0DOC")
	if !ok || err != nil || it.Kind != "document" || it.Filename != "dieta.pdf" || it.Size != 820311 {
		t.Fatalf("get: %+v %v %v", it, ok, err)
	}
	if it.Downloadable.GetDirectPath() != doc.GetDirectPath() || whatsmeow.GetMediaType(it.Downloadable) != whatsmeow.MediaDocument {
		t.Errorf("downloadable = %v", it.Downloadable)
	}
	if _, ok, _ := s.Get("5571999999999@s.whatsapp.net", "nope"); ok {
		t.Error("unknown message found")
	}
	s.now = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	if n, _ := s.Cleanup(7 * 24 * time.Hour); n != 1 {
		t.Errorf("cleanup removed %d, want 1", n)
	}
}

// oggPage builds one Ogg page; the parser does not check CRCs.
func oggPage(seq uint32, granule uint64, payload []byte) []byte {
	h := make([]byte, 27)
	copy(h, "OggS")
	binary.LittleEndian.PutUint64(h[6:], granule)
	binary.LittleEndian.PutUint32(h[18:], seq)
	var segs []byte
	for n := len(payload); ; n -= 255 {
		if n < 255 {
			segs = append(segs, byte(n))
			break
		}
		segs = append(segs, 255)
	}
	h[26] = byte(len(segs))
	return append(append(h, segs...), payload...)
}

func opusFile(preSkip uint16, granule uint64) []byte {
	head := make([]byte, 19)
	copy(head, "OpusHead")
	head[8], head[9] = 1, 1
	binary.LittleEndian.PutUint16(head[10:], preSkip)
	binary.LittleEndian.PutUint32(head[12:], 16000) // input rate: informational, must be ignored
	f := oggPage(0, 0, head)
	f = append(f, oggPage(1, 0, []byte("OpusTags"))...)
	return append(f, oggPage(2, granule, make([]byte, 300))...)
}

func TestOggOpusInfo(t *testing.T) {
	secs, wave, err := OggOpusInfo(opusFile(312, 312+48000*7+100))
	if err != nil || secs != 8 || len(wave) != 64 {
		t.Errorf("got %d s, %d samples, %v; want 8 s (7.002 rounded up), 64 samples", secs, len(wave), err)
	}
	for _, v := range wave {
		if v > 100 {
			t.Fatalf("sample %d out of range", v)
		}
	}
	if _, _, err := OggOpusInfo([]byte("ID3 mp3 data")); err == nil {
		t.Error("an mp3 passed as Ogg")
	}
}

func TestTypeFor(t *testing.T) {
	cases := []struct {
		o    Outgoing
		want whatsmeow.MediaType
		bad  bool
	}{
		{Outgoing{MIME: "image/jpeg"}, whatsmeow.MediaImage, false},
		{Outgoing{MIME: "image/jpeg", AsDocument: true}, whatsmeow.MediaDocument, false},
		{Outgoing{MIME: "video/mp4", Caption: "x"}, whatsmeow.MediaVideo, false},
		{Outgoing{MIME: "application/pdf", Caption: "plano"}, whatsmeow.MediaDocument, false},
		{Outgoing{MIME: "audio/ogg", PTT: true}, whatsmeow.MediaAudio, false},
		{Outgoing{MIME: "audio/mpeg", PTT: true}, "", true},
		{Outgoing{MIME: "audio/ogg", PTT: true, Caption: "x"}, "", true},
		{Outgoing{MIME: "audio/mpeg", Caption: "x"}, "", true},
	}
	for _, c := range cases {
		got, err := TypeFor(c.o)
		if (err != nil) != c.bad || got != c.want {
			t.Errorf("%+v: got %q %v", c.o, got, err)
		}
	}
}

func TestMessage(t *testing.T) {
	up := whatsmeow.UploadResponse{URL: "u", DirectPath: "/d", FileLength: 10}
	quote := &waE2E.ContextInfo{StanzaID: proto.String("3EB0Q")}
	doc := Message(Outgoing{MIME: "application/pdf", Filename: "plano.pdf", Caption: "aqui"}, whatsmeow.MediaDocument, up, 0, nil, quote)
	d := doc.GetDocumentMessage()
	if d.GetFileName() != "plano.pdf" || d.GetCaption() != "aqui" || d.GetContextInfo().GetStanzaID() != "3EB0Q" {
		t.Errorf("document = %v", d)
	}
	voice := Message(Outgoing{MIME: "audio/ogg", PTT: true}, whatsmeow.MediaAudio, up, 8, make([]byte, 64), nil).GetAudioMessage()
	if !voice.GetPTT() || voice.GetSeconds() != 8 || voice.GetMimetype() != VoiceMIME || len(voice.GetWaveform()) != 64 {
		t.Errorf("voice = %v", voice)
	}
	if img := Message(Outgoing{MIME: "image/png"}, whatsmeow.MediaImage, up, 0, nil, nil).GetImageMessage(); img.Caption != nil {
		t.Error("empty caption should be absent")
	}
}

func TestDetectMIMEAndExtension(t *testing.T) {
	if got := DetectMIME("resposta.ogg", nil); got != "audio/ogg" {
		t.Errorf("ogg = %q", got)
	}
	if got := DetectMIME("sem-extensao", []byte("%PDF-1.7")); got != "application/pdf" {
		t.Errorf("pdf by content = %q", got)
	}
	if got := Extension("audio/ogg; codecs=opus"); got != ".ogg" {
		t.Errorf("extension = %q", got)
	}
}

func TestResolveStaysInsideTheMediaDir(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "chat"), 0o755)
	inside := filepath.Join(dir, "chat", "a.pdf")
	os.WriteFile(inside, []byte("x"), 0o644)
	outside := filepath.Join(t.TempDir(), "secret.db")
	os.WriteFile(outside, []byte("x"), 0o644)
	os.Symlink(outside, filepath.Join(dir, "link.db"))

	if got, err := Resolve(dir, "chat/a.pdf"); err != nil || !strings.HasSuffix(got, "chat/a.pdf") {
		t.Errorf("relative inside: %q %v", got, err)
	}
	if _, err := Resolve(dir, inside); err != nil {
		t.Errorf("absolute inside: %v", err)
	}
	for _, p := range []string{outside, "../" + filepath.Base(outside), "link.db", "chat", "missing.pdf", "/etc/passwd"} {
		if got, err := Resolve(dir, p); err == nil {
			t.Errorf("%q accepted as %q", p, got)
		}
	}
}

func TestDestPath(t *testing.T) {
	got := DestPath("/media", "120363000000000001@g.us", "3EB0/../x", "application/pdf", "Plano Final.PDF")
	if got != "/media/120363000000000001_g.us/3EB0_.._x.pdf" {
		t.Errorf("got %q", got)
	}
	if got := DestPath("/media", "123456789@lid", "3EB0A", "audio/ogg; codecs=opus", ""); got != "/media/123456789_lid/3EB0A.ogg" {
		t.Errorf("voice: %q", got)
	}
}

// Regression (A0 on the real phone): with a relative media folder, the download returned
// "media/<chat>/<file>" and sending it back looked for "media/media/…".
func TestDownloadedPathResolvesWithRelativeDir(t *testing.T) {
	t.Chdir(t.TempDir())
	os.Mkdir("media", 0o755)
	p := DestPath("media", "123456789@lid", "3EB0DOC", "application/pdf", "dieta.pdf")
	if !filepath.IsAbs(p) {
		t.Fatalf("DestPath = %q, want absolute", p)
	}
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("%PDF"), 0o644)
	if _, err := Resolve("media", p); err != nil {
		t.Errorf("downloaded path not sendable: %v", err)
	}
}

func TestCleanDir(t *testing.T) {
	dir := t.TempDir()
	old, fresh := filepath.Join(dir, "a", "old.pdf"), filepath.Join(dir, "b", "new.pdf")
	for _, p := range []string{old, fresh} {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	now := time.Now()
	os.Chtimes(old, now.Add(-8*24*time.Hour), now.Add(-8*24*time.Hour))
	n, err := CleanDir(dir, 7*24*time.Hour, now)
	if err != nil || n != 1 {
		t.Fatalf("removed %d, %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a")); !os.IsNotExist(err) {
		t.Error("empty folder left behind")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("fresh file removed")
	}
}
