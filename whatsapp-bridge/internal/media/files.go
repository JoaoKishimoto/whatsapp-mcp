package media

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Resolve returns path (absolute, or relative to dir) as a real path inside dir.
// The gateway only sends files from the shared media volume: unlike the upstream
// /api/send, a caller cannot make it upload /etc/passwd or the session store.
func Resolve(dir, path string) (string, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", errors.New("file not found")
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("file is outside the media directory")
	}
	if fi, err := os.Stat(real); err != nil || !fi.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	return real, nil
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// DestPath is where a received attachment is saved: <dir>/<chat>/<message id><ext>.
// The extension comes from the document's file name, else from the MIME type. The
// path is absolute: the caller may hand it back to Resolve, which reads relative
// paths as relative to dir.
func DestPath(dir, chatJID, messageID, mimeType, filename string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" || len(ext) > 8 {
		ext = Extension(mimeType)
	}
	name := unsafeChars.ReplaceAllString(messageID, "_") + unsafeChars.ReplaceAllString(ext, "")
	return filepath.Join(dir, unsafeChars.ReplaceAllString(chatJID, "_"), name)
}

// CleanDir deletes files older than maxAge under dir, then the folders left empty
// (G11; the bridge is expected to copy what it keeps).
func CleanDir(dir string, maxAge time.Duration, now time.Time) (removed int, err error) {
	cutoff := now.Add(-maxAge)
	var dirs []string
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			if p != dir {
				dirs = append(dirs, p)
			}
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().Before(cutoff) {
			if os.Remove(p) == nil {
				removed++
			}
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- { // deepest first
		os.Remove(dirs[i]) // fails, harmlessly, unless empty
	}
	return removed, err
}
