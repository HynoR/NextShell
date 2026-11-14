// Package instance keeps one process per profile file: the first launch on a database holds a lock,
// and a later launch on the same file asks it to show its window instead of opening the file again.
// It is what mygo's RequestSingleInstanceLock does per app, keyed by the profile's path instead, for
// runs on a profile of their own (--db), which the app-wide lock leaves alone.
package instance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Lock is the lock of one profile file, held by this process or by another.
type Lock struct {
	mu       sync.Mutex
	path     string // the socket
	listener net.Listener
	socket   os.FileInfo // our socket file, which release removes only while it is still ours
	held     bool
	wake     func() // what a later launch asks: show the window
}

// New is the lock of the profile at db; wake runs, on a goroutine of the lock's, when a later launch
// on the same profile asks this one to come forward.
func New(db string, wake func()) *Lock {
	abs, e := filepath.Abs(db)
	if e != nil {
		abs = db
	}
	if real, e := filepath.EvalSymlinks(abs); e == nil {
		abs = real
	}
	return &Lock{path: SocketPath(abs), wake: wake}
}

// SocketPath is a short per-user socket for the profile key: Unix socket paths are limited to about
// 100 bytes.
func SocketPath(key string) string {
	sum := sha256.Sum256([]byte("nextshell-profile\n" + key))
	file := "nextshell-" + strconv.Itoa(os.Getuid()) + "-" + hex.EncodeToString(sum[:8]) + ".sock"
	for _, dir := range []string{os.Getenv("XDG_RUNTIME_DIR"), os.TempDir(), "/tmp"} {
		if dir != "" && len(filepath.Join(dir, file)) < 100 {
			return filepath.Join(dir, file)
		}
	}
	return filepath.Join("/tmp", file)
}

// Take holds the lock and answers true, or, while another process holds it, asks that one to show its
// window and answers false: this launch should then exit. It answers true again once held, and true
// where no lock can be made (a read-only temp dir), as mygo's lock does.
func (l *Lock) Take() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held {
		return true
	}
	for attempt := range 5 {
		if conn, e := net.DialTimeout("unix", l.path, time.Second); e == nil {
			_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
			_, _ = conn.Write([]byte("show\n"))
			if uc, ok := conn.(*net.UnixConn); ok {
				_ = uc.CloseWrite()
			}
			_, _ = io.Copy(io.Discard, conn) // until the holder has read it
			conn.Close()
			return false
		}
		// Nobody answers: a crash left the socket. Take over; a race lost makes Listen fail, and the
		// next attempt reaches the winner.
		_ = os.Remove(l.path)
		ln, e := net.Listen("unix", l.path)
		if e == nil {
			ln.(*net.UnixListener).SetUnlinkOnClose(false)
			l.socket, _ = os.Stat(l.path)
			l.listener, l.held = ln, true
			go l.serve(ln)
			return true
		}
		if !errors.Is(e, syscall.EADDRINUSE) && !errors.Is(e, syscall.Errno(10048)) {
			return true
		}
		time.Sleep(time.Duration(attempt+1) * 20 * time.Millisecond)
	}
	return true
}

func (l *Lock) serve(ln net.Listener) {
	for {
		conn, e := ln.Accept()
		if e != nil {
			return
		}
		go func() {
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
			b, _ := io.ReadAll(io.LimitReader(conn, 64))
			if string(b) == "show\n" && l.wake != nil {
				l.wake()
			}
		}()
	}
}

// Release gives the lock up, removing the socket if it is still this process's.
func (l *Lock) Release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.listener == nil {
		return
	}
	l.listener.Close()
	if fi, e := os.Stat(l.path); e == nil && l.socket != nil && os.SameFile(fi, l.socket) {
		_ = os.Remove(l.path)
	}
	l.listener, l.socket, l.held = nil, nil, false
}
