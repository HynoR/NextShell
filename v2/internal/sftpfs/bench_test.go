package sftpfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Baselines against the in-process x/crypto + pkg/sftp server on loopback: no Docker, no OpenSSH. They
// bound the client's own cost; compare with internal/interop's benchmarks against real sshd.

func BenchmarkLocalUpload100MB(b *testing.B) {
	c, root := client(b)
	local := filepath.Join(b.TempDir(), "payload")
	if err := os.WriteFile(local, make([]byte, 100<<20), 0600); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(100 << 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.Upload(context.Background(), local, filepath.Join(root, "up"), true, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLocalDownload100MB(b *testing.B) {
	c, root := client(b)
	if err := os.WriteFile(filepath.Join(root, "down"), make([]byte, 100<<20), 0600); err != nil {
		b.Fatal(err)
	}
	dir := b.TempDir()
	b.SetBytes(100 << 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.Download(context.Background(), "down", filepath.Join(dir, "out"), true, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLocalList10000Files(b *testing.B) {
	c, root := client(b)
	for i := 0; i < 10000; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%d", i)), nil, 0600); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if es, err := c.List("."); err != nil || len(es) != 10000 {
			b.Fatal(len(es), err)
		}
	}
}
