package transfer

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Client-side cost of packed transfer without any network: Pack (tar+gzip) and Extract (gunzip+untar) of
// 100 files x 1 MiB of random (incompressible) data. Compare with internal/interop's packed benchmarks.
func benchTree(b *testing.B) string {
	b.Helper()
	root := filepath.Join(b.TempDir(), "tree")
	data := make([]byte, 1<<20)
	for i := 0; i < 100; i++ {
		dir := filepath.Join(root, fmt.Sprintf("d%02d", i%10))
		os.MkdirAll(dir, 0755)
		rand.Read(data)
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d", i)), data, 0644)
	}
	return root
}

func BenchmarkPack100MB(b *testing.B) {
	root := benchTree(b)
	b.SetBytes(100 << 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		if err := Pack(context.Background(), root, &out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExtract100MB(b *testing.B) {
	root := benchTree(b)
	var archive bytes.Buffer
	if err := Pack(context.Background(), root, &archive); err != nil {
		b.Fatal(err)
	}
	dir := b.TempDir()
	b.SetBytes(100 << 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dest := filepath.Join(dir, fmt.Sprint(i))
		os.Mkdir(dest, 0700)
		if err := Extract(context.Background(), bytes.NewReader(archive.Bytes()), dest, 10<<30); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		os.RemoveAll(dest)
		b.StartTimer()
	}
}
