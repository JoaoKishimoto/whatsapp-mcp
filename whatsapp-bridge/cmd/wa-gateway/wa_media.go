package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"whatsapp-client/internal/api"
	"whatsapp-client/internal/media"
)

// Media methods of waClient (api.Media).

// DownloadMedia saves a received attachment into the media directory. The file is
// written under a temporary name and renamed, so a failed download leaves nothing behind.
func (w waClient) DownloadMedia(ctx context.Context, chat types.JID, messageID string) (api.Downloaded, error) {
	it, ok, err := w.media.Get(chat.String(), messageID)
	switch {
	case err != nil:
		return api.Downloaded{}, err
	case !ok:
		return api.Downloaded{}, api.ErrMediaUnknown
	case it.Size > api.MaxFileBytes:
		return api.Downloaded{}, api.ErrMediaTooBig
	}
	dest := media.DestPath(w.mediaDir, chat.String(), messageID, it.MIME, it.Filename)
	out := api.Downloaded{Path: dest, Kind: it.Kind, MIME: it.MIME, Filename: it.Filename}
	if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 {
		out.Size = fi.Size() // already downloaded
		return out, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return api.Downloaded{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".download-*")
	if err != nil {
		return api.Downloaded{}, err
	}
	err = w.c.DownloadToFile(ctx, it.Downloadable, tmp)
	tmp.Close()
	if err == nil {
		err = os.Rename(tmp.Name(), dest)
	}
	if err != nil {
		os.Remove(tmp.Name())
		var httpErr whatsmeow.DownloadHTTPError
		if errors.Is(err, whatsmeow.ErrNoURLPresent) ||
			(errors.As(err, &httpErr) && (httpErr.StatusCode == 404 || httpErr.StatusCode == 410)) {
			return api.Downloaded{}, api.ErrMediaGone
		}
		return api.Downloaded{}, err
	}
	os.Chmod(dest, 0o644) // the bridge reads it from its own container
	if fi, err := os.Stat(dest); err == nil {
		out.Size = fi.Size()
	}
	return out, nil
}

// SendFile uploads a file from the media directory and sends it. Encryption needs a
// scratch file; it goes in the media volume, since the container's /tmp is small.
func (w waClient) SendFile(ctx context.Context, to types.JID, path string, o media.Outgoing, reply *api.Reply) (string, error) {
	kind, err := media.TypeFor(o)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var seconds uint32
	var waveform []byte
	if o.PTT {
		data, err := io.ReadAll(io.LimitReader(f, api.MaxFileBytes))
		if err != nil {
			return "", err
		}
		if seconds, waveform, err = media.OggOpusInfo(data); err != nil {
			return "", err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return "", err
		}
	}
	scratch, err := os.CreateTemp(w.mediaDir, ".upload-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(scratch.Name())
	defer scratch.Close()
	up, err := w.c.UploadReader(ctx, f, scratch, kind)
	if err != nil {
		return "", err
	}
	resp, err := w.c.SendMessage(ctx, to, media.Message(o, kind, up, seconds, waveform, reply.Context(w.c.Store.GetJID())))
	return resp.ID, err
}
