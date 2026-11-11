// Package remoteedit uses optimistic hashes and atomic SFTP replacement to avoid silent lost edits.
package remoteedit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"nextshell.dev/v2/internal/sftpfs"
	"os"
	"os/exec"
	"path/filepath"
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
	var d Document
	d.Path = remote
	info, e := c.Lstat(remote)
	if e != nil {
		return d, e
	}
	if !info.Mode().IsRegular() || info.Size() > MaxSize {
		return d, errors.New("editor only accepts regular text files up to 8 MiB")
	}
	f, e := c.Open(remote)
	if e != nil {
		return d, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if e != nil {
		return d, e
	}
	if len(b) > MaxSize || !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return d, errors.New("file is binary or too large")
	}
	d.Text = string(b)
	d.Hash = hash(b)
	d.Mode = info.Mode()
	return d, nil
}
func Save(ctx context.Context, c *sftpfs.Client, d Document, text string) (Document, error) {
	if len(text) > MaxSize || !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
		return d, errors.New("invalid editor text")
	}
	current, e := Open(c, d.Path)
	if e != nil {
		return d, e
	}
	if current.Hash != d.Hash {
		return d, ErrConflict
	}
	if e = c.UploadReader(ctx, strings.NewReader(text), d.Path, true, d.Mode, nil); e != nil {
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

// External cleans private plaintext staging after success; failures retain a recovery file and return its path.
// A save conflicts instead of overwriting a remotely changed file. program is an executable path, not a shell command.
func External(ctx context.Context, c *sftpfs.Client, d Document, watch WatchFactory, program string, onSave func(Document), args ...string) (result error) {
	if strings.TrimSpace(program) == "" {
		return errors.New("external editor executable required")
	}
	dir, e := os.MkdirTemp("", "nextshell-edit-*")
	if e != nil {
		return e
	}
	started := false
	defer func() {
		if started && result != nil {
			result = fmt.Errorf("%w; private recovery file retained at %s", result, filepath.Join(dir, filepath.Base(d.Path)))
			return
		}
		os.RemoveAll(dir)
	}()
	local := filepath.Join(dir, filepath.Base(d.Path))
	if e = os.WriteFile(local, []byte(d.Text), 0600); e != nil {
		return e
	}
	if watch == nil {
		return errors.New("external editor watcher required")
	}
	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()
	watcher, e := watch(watchCtx, local)
	if e != nil {
		return e
	}
	defer watcher.Close()
	changes := make(chan error, 1)
	go func() {
		for {
			err := watcher.Next(watchCtx)
			select {
			case changes <- err:
			case <-watchCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	cmd := exec.CommandContext(ctx, program, append(append([]string{}, args...), local)...)
	if e = cmd.Start(); e != nil {
		return e
	}
	started = true
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	syncFile := func() error {
		info, e := os.Lstat(local)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() || info.Size() > MaxSize {
			return errors.New("external editor file is not bounded regular text")
		}
		f, e := os.Open(local)
		if e != nil {
			return e
		}
		b, e := io.ReadAll(io.LimitReader(f, MaxSize+1))
		f.Close()
		if e != nil {
			return e
		}
		if hash(b) == d.Hash {
			return nil
		}
		next, e := Save(ctx, c, d, string(b))
		if e != nil {
			return e
		}
		d = next
		if onSave != nil {
			onSave(d)
		}
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			cmd.Process.Kill()
			<-done
			return ctx.Err()
		case err := <-done:
			if err != nil {
				return err
			}
			return syncFile()
		case watchErr := <-changes:
			if watchErr != nil {
				cmd.Process.Kill()
				<-done
				return watchErr
			}
			if e = syncFile(); e != nil {
				cmd.Process.Kill()
				<-done
				return e
			}
		}
	}
}
