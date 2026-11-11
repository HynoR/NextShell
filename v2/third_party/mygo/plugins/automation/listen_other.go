//go:build !unix && !mygo_noinspector

package automation

import "net"

// listenPrivate listens on the socket. On Windows, a socket's file has no
// mode: it takes the access control list of its directory, which is the
// user's alone in the user's profile and temporary directory.
func listenPrivate(path string) (net.Listener, error) { return net.Listen("unix", path) }
