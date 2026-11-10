package transfer

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"nextshell.dev/v2/internal/sftpfs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// Pack emits a portable tar.gz tree, rejecting links and special files rather than following them.
func Pack(ctx context.Context, local string, w io.Writer) error {
	info, e := os.Lstat(local)
	if e != nil {
		return e
	}
	if !info.IsDir() {
		return errors.New("packed upload source must be a directory")
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	walkErr := filepath.Walk(local, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return errors.New("packed transfer rejects links/special files")
		}
		rel, e := filepath.Rel(local, p)
		if e != nil {
			return e
		}
		if rel == "." {
			return nil
		}
		h, e := tar.FileInfoHeader(info, "")
		if e != nil {
			return e
		}
		h.Name = filepath.ToSlash(rel)
		if e = tw.WriteHeader(h); e != nil {
			return e
		}
		if info.IsDir() {
			return nil
		}
		f, e := os.Open(p)
		if e != nil {
			return e
		}
		defer f.Close()
		return sftpfs.Copy(ctx, tw, f, nil)
	})
	ce := tw.Close()
	ge := gz.Close()
	if walkErr != nil {
		return walkErr
	}
	if ce != nil {
		return ce
	}
	return ge
}

// Extract writes only into a NEW staging directory: callers atomically rename it into place.
func Extract(ctx context.Context, r io.Reader, destination string, maxBytes int64) error {
	gz, e := gzip.NewReader(r)
	if e != nil {
		return e
	}
	defer gz.Close()
	if maxBytes < 0 || maxBytes > 1<<50 {
		return errors.New("invalid extraction limit")
	}
	budget := &io.LimitedReader{R: gz, N: maxBytes + (64 << 20) + 1}
	tr := tar.NewReader(budget)
	entries := 0
	var total int64
	for {
		h, e := tr.Next()
		if e == io.EOF {
			if e = sftpfs.Copy(ctx, io.Discard, budget, nil); e != nil {
				return e
			}
			if budget.N == 0 {
				return errors.New("archive metadata exceeds limit")
			}
			return nil
		}
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		entries++
		if entries > 100000 || budget.N == 0 {
			return errors.New("archive entry/metadata limit exceeded")
		}
		name := path.Clean(h.Name)
		if name == "." && h.Typeflag == tar.TypeDir {
			continue
		}
		if name == "." || path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "\\") || strings.IndexByte(name, 0) >= 0 {
			return errors.New("unsafe archive path")
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if !strings.HasPrefix(target, filepath.Clean(destination)+string(filepath.Separator)) {
			return errors.New("archive path escapes destination")
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if e = os.MkdirAll(target, 0700); e != nil {
				return e
			}
		case tar.TypeReg, tar.TypeRegA:
			if h.Size < 0 || h.Size > maxBytes-total {
				return errors.New("archive expanded size exceeds limit")
			}
			total += h.Size
			if e = os.MkdirAll(filepath.Dir(target), 0700); e != nil {
				return e
			}
			f, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil {
				return e
			}
			e = sftpfs.Copy(ctx, f, io.LimitReader(tr, h.Size), nil)
			ce := f.Close()
			if e != nil {
				return e
			}
			if ce != nil {
				return ce
			}
		default:
			return fmt.Errorf("unsupported archive entry type %d (links are refused)", h.Typeflag)
		}
	}
}

type Runner interface {
	Run(context.Context, string, io.Reader, io.Writer, io.Writer) error
}

func UploadPacked(ctx context.Context, runner Runner, local, remoteDestination string, progress func(int64)) error {
	if e := sftpfs.CheckPath(remoteDestination); e != nil {
		return e
	}
	if path.Clean(remoteDestination) == "/" || path.Clean(remoteDestination) == "." {
		return errors.New("packed destination must be a new directory")
	} // never merge/overwrite an existing tree
	parent, _ := path.Split(path.Clean(remoteDestination))
	staging := path.Join(parent, ".nextshell-pack-"+uuid.NewString())
	cleanup := "rm -rf -- " + Quote(staging)
	command := "test ! -e " + Quote(remoteDestination) + " && test ! -e " + Quote(staging) + " && mkdir -- " + Quote(staging) + " && { trap " + Quote(cleanup) + " EXIT; tar -xzf - -C " + Quote(staging) + " && mv -T -n -- " + Quote(staging) + " " + Quote(remoteDestination) + " && test ! -e " + Quote(staging) + "; }"
	pr, pw := io.Pipe()
	packErr := make(chan error, 1)
	go func() { e := Pack(ctx, local, pw); pw.CloseWithError(e); packErr <- e }()
	e := runner.Run(ctx, command, &countReader{Reader: pr, progress: progress}, io.Discard, io.Discard)
	pr.CloseWithError(e)
	pe := <-packErr
	if e != nil {
		return e
	}
	if pe != nil {
		return pe
	}
	return nil
}
func DownloadPacked(ctx context.Context, runner Runner, remote, local string, progress func(int64)) error {
	if e := sftpfs.CheckPath(remote); e != nil {
		return e
	}
	if _, e := os.Lstat(local); e == nil {
		return errors.New("packed destination already exists")
	}
	stage, e := os.MkdirTemp(filepath.Dir(local), ".nextshell-unpack-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	command := "tar -czf - -C " + Quote(remote) + " -- ."
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		e := runner.Run(ctx, command, nil, &countWriter{Writer: pw, progress: progress}, io.Discard)
		pw.CloseWithError(e)
		done <- e
	}()
	e = Extract(ctx, pr, stage, 10<<30)
	pr.CloseWithError(e)
	remoteErr := <-done
	if e != nil {
		return e
	}
	if remoteErr != nil {
		return remoteErr
	}
	if e = os.Rename(stage, local); e != nil {
		return e
	}
	return nil
}

type countReader struct {
	io.Reader
	n        int64
	progress func(int64)
}

func (r *countReader) Read(p []byte) (int, error) {
	n, e := r.Reader.Read(p)
	r.n += int64(n)
	if r.progress != nil {
		r.progress(r.n)
	}
	return n, e
}

type countWriter struct {
	io.Writer
	n        int64
	progress func(int64)
}

func (w *countWriter) Write(p []byte) (int, error) {
	n, e := w.Writer.Write(p)
	w.n += int64(n)
	if w.progress != nil {
		w.progress(w.n)
	}
	return n, e
}
