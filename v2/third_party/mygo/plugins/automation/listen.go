//go:build !mygo_noinspector

package automation

import (
	"fmt"
	"net"
	"os"
)

// listen listens on the Unix domain socket at path, which only the user
// can open. A socket left there by an app that ended without closing it
// is removed; one that an app listens on, or a file that is no socket, is
// left alone.
func listen(path string) (net.Listener, error) {
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode().Type() != os.ModeSocket {
			return nil, fmt.Errorf("%s exists and is not a socket", path)
		}
		if c, err := net.Dial("unix", path); err == nil {
			c.Close()
			return nil, fmt.Errorf("another app listens on %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	return listenPrivate(path)
}
