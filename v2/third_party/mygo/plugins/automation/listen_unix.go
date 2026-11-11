//go:build unix && !mygo_noinspector

package automation

import (
	"net"
	"os"
	"syscall"
)

// listenPrivate makes the socket readable and writable by the user alone,
// from the start: with the umask that leaves the others out while the
// socket is made, which runs in init, before goroutines of the app make
// files, and with its mode after.
func listenPrivate(path string) (net.Listener, error) {
	old := syscall.Umask(0o177)
	l, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}
