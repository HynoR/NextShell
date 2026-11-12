package mcpendpoint

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// DefaultPort is where the endpoint listens unless a test asks for another one.
const DefaultPort = "41777"

// DiscoveryFile is the name of the file a local MCP bridge reads to find the endpoint and its token.
const DiscoveryFile = "mcp.json"

type Discovery struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

// WriteDiscovery atomically (temp file + rename) publishes {port, token} at path with mode 0600.
// It refuses to replace a symlink or any other non-regular file, so the token cannot be redirected.
func WriteDiscovery(path string, port int, token string) error {
	if fi, e := os.Lstat(path); e == nil {
		if !fi.Mode().IsRegular() {
			return errors.New("refusing to overwrite non-regular file " + path)
		}
	} else if !errors.Is(e, fs.ErrNotExist) {
		return e
	}
	dir := filepath.Dir(path)
	if e := os.MkdirAll(dir, 0o700); e != nil {
		return e
	}
	raw, e := json.Marshal(Discovery{Port: port, Token: token})
	if e != nil {
		return e
	}
	tmp, e := os.CreateTemp(dir, ".mcp-*.tmp") // O_EXCL, 0600
	if e != nil {
		return e
	}
	_, e = tmp.Write(raw)
	if e == nil {
		e = tmp.Chmod(0o600) // no-op on Windows
	}
	if e == nil {
		e = tmp.Sync()
	}
	if c := tmp.Close(); e == nil {
		e = c
	}
	if e == nil {
		e = os.Rename(tmp.Name(), path)
	}
	if e != nil {
		os.Remove(tmp.Name())
	}
	return e
}

// RemoveDiscovery deletes the file. A missing file is fine; a symlink or other non-regular file is left alone.
func RemoveDiscovery(path string) error {
	fi, e := os.Lstat(path)
	if errors.Is(e, fs.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	if !fi.Mode().IsRegular() {
		return errors.New("refusing to remove non-regular file " + path)
	}
	return os.Remove(path)
}
