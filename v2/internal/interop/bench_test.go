//go:build interop

package interop

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nextshell.dev/v2/internal/sftpfs"
	"nextshell.dev/v2/internal/sshclient"
	"nextshell.dev/v2/internal/transfer"
)

// Run: go test -tags interop -run '^$' -bench . -benchtime=1x ./internal/interop/
// Numbers include Docker-for-Mac's port forwarding and OpenSSH on an arm64 Linux VM; see docs/v2/PERF.md.

const mib = 1 << 20

func benchSFTP(b *testing.B) (*sftpfs.Client, *sshclient.Session) {
	b.Helper()
	return sftpOn(b, Shared(b, Debian))
}

func BenchmarkDial(b *testing.B) {
	c := Shared(b, Debian)
	for name, o := range map[string]sshclient.Options{"password": c.Options(), "ed25519": c.KeyOptions(Keys.Ed25519), "rsa2048": c.KeyOptions(Keys.RSA)} {
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				s, err := sshclient.Dial(context.Background(), o) // TCP + kex + auth + pty + shell
				if err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				s.Close()
				b.StartTimer()
			}
		})
	}
}

func BenchmarkSFTPList10000(b *testing.B) {
	sc, s := benchSFTP(b)
	Sh(b, s, `rm -rf ~/ten && mkdir ~/ten && cd ~/ten && seq 1 10000 | sed 's/^/file-/' | xargs touch`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entries, err := sc.List("/home/tester/ten")
		if err != nil || len(entries) != 10000 {
			b.Fatal(len(entries), err)
		}
	}
}

func randomFile(b *testing.B, size int) string {
	b.Helper()
	p := filepath.Join(b.TempDir(), "payload")
	f, err := os.Create(p)
	if err != nil {
		b.Fatal(err)
	}
	chunk := make([]byte, mib)
	for written := 0; written < size; written += len(chunk) {
		crand.Read(chunk)
		f.Write(chunk[:min(len(chunk), size-written)])
	}
	f.Close()
	return p
}

func BenchmarkSFTPUpload100MB(b *testing.B) {
	sc, s := benchSFTP(b)
	local := randomFile(b, 100*mib)
	b.SetBytes(100 * mib)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := sc.Upload(context.Background(), local, "/home/tester/up100", true, nil); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if got := strings.Fields(Sh(b, s, "stat -c %s ~/up100"))[0]; got != fmt.Sprint(100*mib) {
		b.Fatal("size", got)
	}
}

func BenchmarkSFTPDownload100MB(b *testing.B) {
	sc, s := benchSFTP(b)
	Sh(b, s, "head -c 104857600 /dev/urandom > ~/down100")
	dir := b.TempDir()
	b.SetBytes(100 * mib)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := sc.Download(context.Background(), "/home/tester/down100", filepath.Join(dir, "out"), true, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// packedTree builds 100 files x 1 MiB; compressible=false is random bytes (worst case for gzip: pure CPU
// cost), true is repetitive text (best case: little on the wire).
func packedTree(b *testing.B, compressible bool) string {
	b.Helper()
	root := filepath.Join(b.TempDir(), "tree")
	r := rand.New(rand.NewPCG(7, 7))
	text := bytes.Repeat([]byte("the quick brown fox 跳过 lazy dog\n"), mib/36+1)[:mib]
	for i := 0; i < 100; i++ {
		dir := filepath.Join(root, fmt.Sprintf("d%02d", i%10))
		os.MkdirAll(dir, 0755)
		data := text
		if !compressible {
			data = make([]byte, mib)
			for j := 0; j < len(data); j += 8 {
				v := r.Uint64()
				for k := 0; k < 8 && j+k < len(data); k++ {
					data[j+k] = byte(v >> (8 * k))
				}
			}
		}
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d", i)), data, 0644)
	}
	return root
}

func BenchmarkPackedUpload100x1MB(b *testing.B) {
	_, s := benchSFTP(b)
	for _, kind := range []string{"random", "text"} {
		b.Run(kind, func(b *testing.B) {
			local := packedTree(b, kind == "text")
			b.SetBytes(100 * mib)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				Sh(b, s, "rm -rf ~/packed100")
				b.StartTimer()
				if err := transfer.UploadPacked(context.Background(), s, local, "/home/tester/packed100", nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPackedDownload100x1MB(b *testing.B) {
	_, s := benchSFTP(b)
	for _, kind := range []string{"random", "text"} {
		b.Run(kind, func(b *testing.B) {
			src := "head -c 1048576 /dev/urandom"
			if kind == "text" {
				src = `yes 'the quick brown fox 跳过 lazy dog' | head -c 1048576`
			}
			Sh(b, s, `rm -rf ~/pd100 && mkdir -p ~/pd100 && for i in $(seq 1 100); do mkdir -p ~/pd100/d$((i%10)); `+src+` > ~/pd100/d$((i%10))/f$i; done`)
			dir := b.TempDir()
			b.SetBytes(100 * mib)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				dest := filepath.Join(dir, fmt.Sprint("out", i))
				if err := transfer.DownloadPacked(context.Background(), s, "/home/tester/pd100", dest, nil); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				os.RemoveAll(dest)
				b.StartTimer()
			}
		})
	}
}

// BenchmarkSessionOutput200MB drains a PTY shell session the way the terminal view does (Session.Read into
// a 64 KiB buffer) while the remote prints 200 MB. No terminal emulator is attached: this is the
// transport + decoder ceiling, not rendering. "lines80" is log/cat-like text (79 chars + newline);
// "newline-heavy" is `yes`, one newline per 2 bytes, which is bound by the remote kernel's tty
// line-discipline (ONLCR) rather than SSH: OpenSSH's own client is just as slow there (see PERF.md).
func BenchmarkSessionOutput200MB(b *testing.B) {
	c := Shared(b, Debian)
	line := strings.Repeat("x", 79)
	for _, tc := range []struct{ name, enc, cmd string }{
		{"lines80/utf-8", "utf-8", "yes " + line + " | head -c 200000000"},
		{"lines80/gbk", "gbk", "yes " + line + " | head -c 200000000"},
		{"newline-heavy/utf-8", "utf-8", "yes | head -c 200000000"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			o := c.Options()
			o.Connection.Encoding = tc.enc
			s := Dial(b, o)
			buf := make([]byte, 64<<10)
			drainUntil := func(marker string) (int64, error) {
				var total int64
				var tail []byte
				for {
					n, err := s.Read(buf)
					total += int64(n)
					window := append(tail, buf[:n]...)
					if bytes.Contains(window, []byte(marker)) {
						return total, nil
					}
					tail = append(tail[:0], window[max(0, len(window)-len(marker)):]...)
					if err != nil {
						return total, err
					}
				}
			}
			if _, err := s.Write([]byte("echo READY$((1+1))\n")); err != nil {
				b.Fatal(err)
			}
			if _, err := drainUntil("READY2"); err != nil {
				b.Fatal(err)
			}
			var bytesRead int64
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := s.Write([]byte(tc.cmd + "; echo DONE$((1+1))\n")); err != nil {
					b.Fatal(err)
				}
				n, err := drainUntil("DONE2")
				if err != nil {
					b.Fatal(err)
				}
				bytesRead = n
			}
			b.SetBytes(bytesRead)
			b.ReportMetric(float64(bytesRead)/mib, "MiB/op")
		})
	}
}

// BenchmarkExecChannel200MB: same volume over an exec channel (what monitor/Output uses), no PTY, no decoding.
func BenchmarkExecChannel200MB(b *testing.B) {
	s := Dial(b, Shared(b, Debian).Options())
	b.SetBytes(200 * 1000 * 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.Run(context.Background(), "head -c 200000000 /dev/zero", nil, io.Discard, io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSFTPLatency shows how SFTP throughput degrades with round-trip time (a delaying TCP proxy sits in
// front of sshd). A transfer that keeps many requests in flight barely notices; one that waits for each
// 32 KiB write acknowledgement is capped at 32 KiB / RTT.
func BenchmarkSFTPLatency(b *testing.B) {
	c := Shared(b, Debian)
	const size = 16 * mib
	local := randomFile(b, size)
	for _, rtt := range []time.Duration{0, 20 * time.Millisecond, 100 * time.Millisecond} {
		b.Run(fmt.Sprintf("rtt=%v", rtt), func(b *testing.B) {
			p := newTCPProxy(b, c.Addr(22), rtt/2)
			s := Dial(b, viaProxy(c.Options(), p))
			sf, cleanup, err := s.OpenSFTPContext(context.Background())
			if err != nil {
				b.Fatal(err)
			}
			defer cleanup()
			sc := &sftpfs.Client{Client: sf}
			b.Run("upload16MB", func(b *testing.B) {
				b.SetBytes(size)
				for i := 0; i < b.N; i++ {
					if err := sc.Upload(context.Background(), local, "/home/tester/wan-up", true, nil); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("download16MB", func(b *testing.B) {
				out := filepath.Join(b.TempDir(), "out")
				b.SetBytes(size)
				for i := 0; i < b.N; i++ {
					if err := sc.Download(context.Background(), "/home/tester/wan-up", out, true, nil); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

// BenchmarkSmallFiles uploads 100 files x 4 KiB: one SFTP upload per file (what CopyTree / a per-file
// queue does: stat, open, write, close, chmod, link/rename, remove-temp = ~8 round trips each) against one
// packed tar.gz stream. Per-file cost is paid in round trips, so it explodes with latency.
func BenchmarkSmallFiles(b *testing.B) {
	c := Shared(b, Debian)
	root := filepath.Join(b.TempDir(), "tree")
	os.MkdirAll(root, 0755)
	payload := make([]byte, 4096)
	for i := 0; i < 100; i++ {
		crand.Read(payload)
		os.WriteFile(filepath.Join(root, fmt.Sprintf("f%03d", i)), payload, 0644)
	}
	for _, rtt := range []time.Duration{0, 20 * time.Millisecond} {
		p := newTCPProxy(b, c.Addr(22), rtt/2)
		s := Dial(b, viaProxy(c.Options(), p))
		sf, cleanup, err := s.OpenSFTPContext(context.Background())
		if err != nil {
			b.Fatal(err)
		}
		defer cleanup()
		sc := &sftpfs.Client{Client: sf}
		b.Run(fmt.Sprintf("rtt=%v/sftp-per-file", rtt), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				Sh(b, s, "rm -rf ~/small && mkdir ~/small")
				b.StartTimer()
				for j := 0; j < 100; j++ {
					name := fmt.Sprintf("f%03d", j)
					if err := sc.Upload(context.Background(), filepath.Join(root, name), "/home/tester/small/"+name, false, nil); err != nil {
						b.Fatal(err)
					}
				}
			}
			b.ReportMetric(float64(b.Elapsed().Milliseconds())/float64(b.N)/100, "ms/file")
		})
		b.Run(fmt.Sprintf("rtt=%v/packed", rtt), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				Sh(b, s, "rm -rf ~/small")
				b.StartTimer()
				if err := transfer.UploadPacked(context.Background(), s, root, "/home/tester/small", nil); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Milliseconds())/float64(b.N)/100, "ms/file")
		})
	}
}
