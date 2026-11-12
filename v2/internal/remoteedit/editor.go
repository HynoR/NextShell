// Package remoteedit uses optimistic hashes and atomic SFTP replacement to avoid silent lost edits.
package remoteedit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"nextshell.dev/v2/internal/sftpfs"
	"os"
	"strings"
	"unicode/utf8"
)

var ErrConflict = errors.New("remote file changed since open; reload before saving")

const MaxSize = 8 << 20

type Document struct {
	Path, Text, Hash string
	Mode             os.FileMode
}

func hash(b []byte) string { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }
func Open(c *sftpfs.Client, remote string) (Document, error) {
	d, _, e := open(c, remote)
	return d, e
}

// open reads remote, a link included: it returns the document and the path that holds the text, which
// is where a save must go so that a link stays a link.
func open(c *sftpfs.Client, remote string) (Document, string, error) {
	var d Document
	d.Path = remote
	real, info, e := c.Resolve(remote)
	if e != nil {
		return d, "", e
	}
	if !info.Mode().IsRegular() || info.Size() > MaxSize {
		return d, "", errors.New("editor only accepts regular text files up to 8 MiB")
	}
	f, e := c.Open(real)
	if e != nil {
		return d, "", e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if e != nil {
		return d, "", e
	}
	if len(b) > MaxSize || !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return d, "", errors.New("file is binary or too large")
	}
	d.Text = string(b)
	d.Hash = hash(b)
	d.Mode = info.Mode()
	return d, real, nil
}
func Save(ctx context.Context, c *sftpfs.Client, d Document, text string) (Document, error) {
	if len(text) > MaxSize || !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
		return d, errors.New("invalid editor text")
	}
	current, real, e := open(c, d.Path)
	if e != nil {
		return d, e
	}
	if current.Hash != d.Hash {
		return d, ErrConflict
	}
	if e = c.UploadReader(ctx, strings.NewReader(text), real, true, d.Mode, nil); e != nil {
		return d, e
	}
	d.Text = text
	d.Hash = hash([]byte(text))
	return d, nil
}

// Watcher is supplied by Platform; core never depends on a desktop toolkit.
type Watcher interface {
	Next(context.Context) error
	Close() error
}
type WatchFactory func(context.Context, string) (Watcher, error)
