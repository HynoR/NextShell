// Package sftpfs provides bounded, atomic transfer primitives on SSH's SFTP subsystem.
package sftpfs

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/pkg/sftp"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type Client struct{ *sftp.Client }
type Entry struct {
	Name, Path         string
	Size               int64
	Mode               string
	Directory, Symlink bool
	Modified           time.Time
}

func (c *Client) List(remote string) ([]Entry, error) {
	entries, e := c.ReadDir(remote)
	if e != nil {
		return nil, e
	}
	out := make([]Entry, 0, len(entries))
	for _, v := range entries {
		out = append(out, Entry{Name: v.Name(), Path: path.Join(remote, v.Name()), Size: v.Size(), Mode: v.Mode().String(), Directory: v.IsDir(), Symlink: v.Mode()&os.ModeSymlink != 0, Modified: v.ModTime()})
	}
	return out, nil
}
func CheckPath(p string) error {
	if strings.IndexByte(p, 0) >= 0 || strings.TrimSpace(p) == "" {
		return errors.New("invalid remote path")
	}
	return nil
}
func (c *Client) RemovePath(p string) error {
	if e := CheckPath(p); e != nil {
		return e
	}
	if path.Clean(p) == "/" || path.Clean(p) == "." {
		return errors.New("refusing root removal")
	}
	v, e := c.Lstat(p)
	if e != nil {
		return e
	}
	if v.IsDir() {
		return c.RemoveDirectory(p)
	}
	return c.Remove(p)
}
func (c *Client) RenamePath(from, to string) error {
	if e := CheckPath(from); e != nil {
		return e
	}
	if e := CheckPath(to); e != nil {
		return e
	}
	if path.Clean(from) == "/" || path.Clean(from) == "." {
		return errors.New("refusing root rename")
	}
	if _, e := c.Lstat(to); e == nil {
		return errors.New("rename target exists")
	}
	return c.Rename(from, to)
}
func Copy(ctx context.Context, w io.Writer, r io.Reader, progress func(int64)) error {
	buf := make([]byte, 128*1024)
	var total int64
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		n, e := r.Read(buf)
		if n > 0 {
			written, we := w.Write(buf[:n])
			total += int64(written)
			if progress != nil {
				progress(total)
			}
			if we != nil {
				return we
			}
			if written != n {
				return io.ErrShortWrite
			}
		}
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return e
		}
	}
}

// ctxReader/ctxWriter make pkg/sftp's pipelined ReadFrom/WriteTo cancellable and observable. Those keep up
// to 64 x 32 KiB requests in flight. The Read/Write loop this replaces waited one round trip per 32 KiB
// (write) or per 128 KiB (read): at 20 ms RTT that was 1.4 MB/s up and 5.5 MB/s down instead of line rate.
type ctxReader struct {
	ctx      context.Context
	r        io.Reader
	n        int64
	progress func(int64)
}

func (r *ctxReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	n, e := r.r.Read(p)
	r.n += int64(n)
	if n > 0 && r.progress != nil {
		r.progress(r.n)
	}
	return n, e
}

type ctxWriter struct {
	ctx      context.Context
	w        io.Writer
	n        int64
	progress func(int64)
}

func (w *ctxWriter) Write(p []byte) (int, error) {
	if e := w.ctx.Err(); e != nil {
		return 0, e
	}
	n, e := w.w.Write(p)
	w.n += int64(n)
	if n > 0 && w.progress != nil {
		w.progress(w.n)
	}
	return n, e
}

// push streams r into the remote file with pipelined writes. A remote source (*sftp.File, as in
// CopyTree/Cross) is read through a pipe fed by its own pipelined WriteTo, so neither side waits a round
// trip per chunk. Concurrent writes can leave holes after an error; the caller only publishes the file on
// success and removes it otherwise.
func push(ctx context.Context, f *sftp.File, r io.Reader, progress func(int64)) error {
	if src, ok := r.(*sftp.File); ok {
		pr, pw := io.Pipe()
		done := make(chan struct{})
		go func() { _, e := src.WriteTo(pw); pw.CloseWithError(e); close(done) }()
		defer func() { pr.CloseWithError(context.Canceled); <-done }()
		r = pr
	}
	_, e := f.ReadFromWithConcurrency(&ctxReader{ctx: ctx, r: r, progress: progress}, 0)
	return e
}
func (c *Client) UploadReader(ctx context.Context, r io.Reader, remote string, overwrite bool, mode os.FileMode, progress func(int64)) error {
	if e := CheckPath(remote); e != nil {
		return e
	}
	if old, e := c.Lstat(remote); e == nil {
		if !overwrite {
			return errors.New("remote destination exists")
		}
		if !old.Mode().IsRegular() {
			return errors.New("remote destination is not a regular file")
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	temp := remote + ".nextshell-" + uuid.NewString() + ".part"
	f, e := c.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if e != nil {
		return e
	}
	defer c.Remove(temp)
	e = push(ctx, f, r, progress)
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if e = c.Chmod(temp, mode.Perm()&0777); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if overwrite {
		if _, ok := c.HasExtension("posix-rename@openssh.com"); ok {
			return c.PosixRename(temp, remote)
		}
		return fmt.Errorf("server lacks atomic overwrite extension; original destination left intact")
	}
	if _, e = c.Lstat(remote); e == nil {
		return errors.New("remote destination appeared during upload")
	}
	if _, ok := c.HasExtension("hardlink@openssh.com"); ok {
		return c.Link(temp, remote)
	}
	return c.Rename(temp, remote)
}
func (c *Client) Upload(ctx context.Context, local, remote string, overwrite bool, progress func(int64)) error {
	info, e := os.Lstat(local)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() {
		return errors.New("upload source must be a regular file, not a symlink/directory")
	}
	f, e := os.Open(local)
	if e != nil {
		return e
	}
	defer f.Close()
	return c.UploadReader(ctx, f, remote, overwrite, info.Mode(), progress)
}
func (c *Client) Download(ctx context.Context, remote, local string, overwrite bool, progress func(int64)) error {
	info, e := c.Lstat(remote)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() {
		return errors.New("download source must be a regular file")
	}
	if _, e = os.Lstat(local); e == nil && !overwrite {
		return errors.New("local destination exists")
	}
	r, e := c.Open(remote)
	if e != nil {
		return e
	}
	defer r.Close()
	temp, e := os.CreateTemp(filepath.Dir(local), ".nextshell-*.part")
	if e != nil {
		return e
	}
	tmp := temp.Name()
	defer os.Remove(tmp)
	_, e = r.WriteTo(&ctxWriter{ctx: ctx, w: temp, progress: progress})
	if ce := temp.Close(); e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if overwrite {
		return os.Rename(tmp, local)
	}
	if e = os.Link(tmp, local); e != nil {
		return e
	}
	return nil
}
func Cross(ctx context.Context, from, to *Client, source, destination string, overwrite bool, progress func(int64)) error {
	v, e := from.Lstat(source)
	if e != nil {
		return e
	}
	if !v.Mode().IsRegular() {
		return errors.New("cross-server source must be regular")
	}
	r, e := from.Open(source)
	if e != nil {
		return e
	}
	defer r.Close()
	return to.UploadReader(ctx, r, destination, overwrite, v.Mode(), progress)
}
