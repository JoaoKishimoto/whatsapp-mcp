package media

import (
	"errors"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// VoiceMIME is what WhatsApp expects for a voice note.
const VoiceMIME = "audio/ogg; codecs=opus"

// Outgoing is a file to send. MIME may be empty: DetectMIME fills it.
type Outgoing struct {
	MIME       string
	Filename   string // shown for documents
	Caption    string
	PTT        bool // send as a voice note (Ogg/Opus only)
	AsDocument bool // keep images and videos uncompressed
}

// TypeFor picks how the file goes: images, videos and audio as such, anything else
// (or AsDocument) as a document.
func TypeFor(o Outgoing) (whatsmeow.MediaType, error) {
	base, _, _ := mime.ParseMediaType(o.MIME)
	switch {
	case o.PTT:
		if base != "audio/ogg" {
			return "", errors.New("a voice note must be Ogg/Opus (audio/ogg)")
		}
		if o.Caption != "" {
			return "", errors.New("a voice note cannot have a caption")
		}
		return whatsmeow.MediaAudio, nil
	case o.AsDocument:
		return whatsmeow.MediaDocument, nil
	case base == "image/jpeg" || base == "image/png" || base == "image/webp":
		return whatsmeow.MediaImage, nil
	case base == "video/mp4" || base == "video/3gpp":
		return whatsmeow.MediaVideo, nil
	case strings.HasPrefix(base, "audio/"):
		if o.Caption != "" {
			return "", errors.New("audio cannot have a caption; send it as a document")
		}
		return whatsmeow.MediaAudio, nil
	}
	return whatsmeow.MediaDocument, nil
}

// Message builds the WhatsApp message for an uploaded file. seconds and waveform are
// for voice notes (OggOpusInfo); quote, if set, makes it a reply.
func Message(o Outgoing, t whatsmeow.MediaType, up whatsmeow.UploadResponse, seconds uint32, waveform []byte, quote *waE2E.ContextInfo) *waE2E.Message {
	caption := optional(o.Caption)
	switch t {
	case whatsmeow.MediaImage:
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			Caption: caption, Mimetype: proto.String(o.MIME), ContextInfo: quote,
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength,
		}}
	case whatsmeow.MediaVideo:
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			Caption: caption, Mimetype: proto.String(o.MIME), ContextInfo: quote,
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength,
		}}
	case whatsmeow.MediaAudio:
		a := &waE2E.AudioMessage{
			Mimetype: proto.String(o.MIME), ContextInfo: quote,
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength,
		}
		if o.PTT {
			a.Mimetype, a.PTT, a.Seconds, a.Waveform = proto.String(VoiceMIME), proto.Bool(true), proto.Uint32(seconds), waveform
		}
		return &waE2E.Message{AudioMessage: a}
	}
	return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		Caption: caption, Mimetype: proto.String(o.MIME), ContextInfo: quote,
		FileName: proto.String(o.Filename), Title: proto.String(o.Filename),
		URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
		FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength,
	}}
}

// extensions maps the types the gateway sends and receives most; Go's built-in table
// (and distroless, which has no /etc/mime.types) misses several of them.
var extensions = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif",
	"video/mp4": ".mp4", "video/3gpp": ".3gp",
	"audio/ogg": ".ogg", "audio/mpeg": ".mp3", "audio/mp4": ".m4a", "audio/aac": ".aac",
	"application/pdf": ".pdf",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   ".docx",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         ".xlsx",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx",
	"text/plain": ".txt", "text/csv": ".csv",
}

// DetectMIME guesses a file's type from its name, then from its first bytes.
func DetectMIME(name string, head []byte) string {
	ext := strings.ToLower(filepath.Ext(name))
	for t, e := range extensions {
		if e == ext {
			return t
		}
	}
	if ext == ".opus" || ext == ".oga" {
		return "audio/ogg"
	}
	return http.DetectContentType(head)
}

// Extension returns the file extension for a MIME type, or "" if unknown.
func Extension(mimeType string) string {
	base, _, _ := mime.ParseMediaType(mimeType)
	return extensions[base]
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
