package sftpfs

import (
	"bytes"
	"context"
	"net"
	"nextshell.dev/v2/internal/sshclient"
	"nextshell.dev/v2/internal/testutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// delayProxy forwards TCP with a fixed one-way delay (order and throughput preserved): a WAN in a box.
func delayProxy(t *testing.T, target string, oneWay time.Duration) string {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { l.Close() })
	type chunk struct {
		b   []byte
		due time.Time
	}
	pipe := func(src, dst net.Conn) {
		q := make(chan chunk, 4096)
		go func() {
			defer close(q)
			for {
				b := make([]byte, 32<<10)
				n, e := src.Read(b)
				if n > 0 {
					q <- chunk{b[:n], time.Now().Add(oneWay)}
				}
				if e != nil {
					return
				}
			}
		}()
		go func() {
			defer dst.Close()
			for c := range q {
				time.Sleep(time.Until(c.due))
				dst.Write(c.b)
			}
		}()
	}
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			up, e := net.Dial("tcp", target)
			if e != nil {
				c.Close()
				continue
			}
			t.Cleanup(func() { c.Close(); up.Close() })
			pipe(c, up)
			pipe(up, c)
		}
	}()
	return l.Addr().String()
}

// A 20 ms RTT link must not cap SFTP at 32 KiB / RTT (1.6 MB/s): sequential 32 KiB writes would need
// ~5 s for these 8 MiB, pipelined requests need a fraction of a second.
func TestTransfersAreNotRoundTripBound(t *testing.T) {
	testutil.RequirePOSIXRemote(t)
	root := t.TempDir()
	server, e := testutil.New(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(server.Close)
	o := server.Options()
	host, port, _ := net.SplitHostPort(delayProxy(t, server.Address, 10*time.Millisecond))
	o.Connection.Host = host
	o.Connection.Port, _ = strconv.Atoi(port)
	session, e := sshclient.Dial(context.Background(), o)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { session.Close() })
	raw, cleanup, e := session.OpenSFTPContext(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(cleanup)
	c := &Client{raw}

	data := bytes.Repeat([]byte("0123456789abcdef"), 8<<16) // 8 MiB
	local := filepath.Join(t.TempDir(), "src")
	if e = os.WriteFile(local, data, 0600); e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	if e = c.Upload(context.Background(), local, filepath.Join(root, "up"), false, nil); e != nil {
		t.Fatal(e)
	}
	up := time.Since(start)
	start = time.Now()
	back := filepath.Join(t.TempDir(), "back")
	if e = c.Download(context.Background(), filepath.Join(root, "up"), back, false, nil); e != nil {
		t.Fatal(e)
	}
	down := time.Since(start)
	t.Logf("8 MiB at 20 ms RTT: upload %v, download %v", up.Round(time.Millisecond), down.Round(time.Millisecond))
	if up > 2*time.Second || down > time.Second { // measured ~0.25 s each; before the fix 5.9 s up, 1.5 s down
		t.Fatalf("transfers look round-trip bound: upload %v download %v", up, down)
	}
	if got, _ := os.ReadFile(back); !bytes.Equal(got, data) {
		t.Fatal("corrupt round trip")
	}
}
