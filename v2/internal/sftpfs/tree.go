package sftpfs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
)

// CopyTree streams a file/directory using pkg/sftp, without a remote shell or tar.
// It publishes a private staged payload only after success. It never merges or
// overwrites existing destinations, follows source symlinks, or deletes source.
// sameServer must be true when both clients refer to the same host/account.
func CopyTree(ctx context.Context, source, target *Client, from, to string, sameServer bool, progress func(int64)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := CheckPath(from); err != nil {
		return err
	}
	if err := CheckPath(to); err != nil {
		return err
	}
	from = path.Clean(from)
	to = path.Clean(to)
	if from == "/" || from == "." || to == "/" || to == "." {
		return errors.New("refuse root/current-directory copy")
	}
	info, err := source.Lstat(from)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("copying symlinks is unsupported")
	}
	realFrom, err := source.RealPath(from)
	if err != nil {
		return err
	}
	parent, err := target.RealPath(path.Dir(to))
	if err != nil {
		return err
	}
	to = path.Join(parent, path.Base(to))
	if sameServer && (to == realFrom || strings.HasPrefix(to, strings.TrimSuffix(realFrom, "/")+"/")) {
		return errors.New("cannot copy into source tree")
	}
	if _, err = target.Lstat(to); err == nil {
		return fs.ErrExist
	} else if !os.IsNotExist(err) {
		return err
	}
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		return err
	}
	stage := path.Join(parent, ".nextshell-copy-"+hex.EncodeToString(random))
	if err = target.Mkdir(stage); err != nil {
		return err
	}
	// Only paths that this operation created are cleaned up, in reverse order.
	type created struct {
		path string
		dir  bool
	}
	owned := []created{{stage, true}}
	defer func() {
		for _, node := range owned {
			if node.dir {
				_ = target.Chmod(node.path, 0700)
			}
		}
		for i := len(owned) - 1; i >= 0; i-- {
			if owned[i].dir {
				_ = target.RemoveDirectory(owned[i].path)
			} else {
				_ = target.Remove(owned[i].path)
			}
		}
	}()
	if err = target.Chmod(stage, 0700); err != nil {
		return err
	}
	entries := 0
	var total int64
	var copyOne func(string, string, int) error
	copyOne = func(src, dst string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries++
		if depth > 64 || entries > 10000 {
			return errors.New("copy limit: depth 64 / 10000 entries")
		}
		st, err := source.Lstat(src)
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink not copied: %s", src)
		}
		if st.IsDir() {
			if err = target.Mkdir(dst); err != nil {
				return err
			}
			owned = append(owned, created{dst, true})
			if err = target.Chmod(dst, 0700); err != nil {
				return err
			}
			children, err := source.ReadDir(src)
			if err != nil {
				return err
			}
			for _, child := range children {
				name := child.Name()
				if name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") || name == "" {
					return errors.New("invalid remote entry name")
				}
				if err = copyOne(path.Join(src, name), path.Join(dst, name), depth+1); err != nil {
					return err
				}
			}
			return target.Chmod(dst, st.Mode().Perm())
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("special file not copied: %s", src)
		}
		input, err := source.Open(src)
		if err != nil {
			return err
		}
		defer input.Close()
		base := total
		err = target.UploadReader(ctx, input, dst, false, st.Mode().Perm(), func(n int64) {
			if progress != nil {
				progress(base + n)
			}
		})
		if err == nil {
			owned = append(owned, created{dst, false})
			total += st.Size()
		}
		return err
	}
	payload := path.Join(stage, "payload")
	if err = copyOne(from, payload, 0); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return target.RenamePath(payload, to)
}
