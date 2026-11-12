package sftpfs

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"nextshell.dev/v2/internal/model"
)

// UploadTree copies a local directory to a new remote one file by file, for
// servers without tar. It never merges into an existing destination and skips
// symlinks. progress gets the bytes sent so far.
func (c *Client) UploadTree(ctx context.Context, local, remote string, progress func(int64)) error {
	if e := CheckPath(remote); e != nil {
		return e
	}
	if _, e := c.Lstat(remote); e == nil {
		return fs.ErrExist
	}
	var done int64
	return filepath.WalkDir(local, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		rel, err := filepath.Rel(local, p)
		if err != nil {
			return err
		}
		target := path.Join(remote, filepath.ToSlash(rel))
		switch {
		case d.IsDir():
			return c.Mkdir(target)
		case !d.Type().IsRegular():
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		base := done
		if err = c.UploadReader(ctx, f, target, false, info.Mode(), func(n int64) { progress(base + n) }); err != nil {
			return err
		}
		done += info.Size()
		return nil
	})
}

// DownloadTree copies a remote directory to a new local one file by file, for
// servers without tar. It never merges into an existing destination and skips symlinks.
func (c *Client) DownloadTree(ctx context.Context, remote, local string, progress func(int64)) error {
	if e := CheckPath(remote); e != nil {
		return e
	}
	if _, e := os.Lstat(local); e == nil {
		return errors.New("local destination exists")
	}
	var done int64
	var walk func(from, to string) error
	walk = func(from, to string) error {
		if e := os.Mkdir(to, 0o755); e != nil {
			return e
		}
		entries, e := c.ReadDir(from)
		if e != nil {
			return e
		}
		for _, v := range entries {
			if e := ctx.Err(); e != nil {
				return e
			}
			src, dst := path.Join(from, v.Name()), filepath.Join(to, v.Name())
			if e := model.ValidateDownloadName(v.Name()); e != nil {
				return e
			}
			switch {
			case v.IsDir():
				e = walk(src, dst)
			case v.Mode().IsRegular():
				base := done
				e = c.Download(ctx, src, dst, false, func(n int64) { progress(base + n) })
				done += v.Size()
			}
			if e != nil {
				return e
			}
		}
		return nil
	}
	return walk(remote, local)
}
