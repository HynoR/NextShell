package mcpendpoint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func readDiscovery(t *testing.T, path string) Discovery {
	t.Helper()
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var d Discovery
	if e = json.Unmarshal(raw, &d); e != nil {
		t.Fatal(e, string(raw))
	}
	return d
}

func TestDiscoveryWriteIsPrivateExactAndLeavesNoTemp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", DiscoveryFile)
	if e := WriteDiscovery(path, 41777, "token-a"); e != nil {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != `{"port":41777,"token":"token-a"}` {
		t.Fatal(string(raw))
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Fatal("mode", fi.Mode())
		}
		if fi, _ := os.Stat(filepath.Dir(path)); fi.Mode().Perm() != 0o700 {
			t.Fatal("dir mode", fi.Mode())
		}
	}
	if e := WriteDiscovery(path, 41778, "token-b"); e != nil { // rotation rewrite
		t.Fatal(e)
	}
	if d := readDiscovery(t, path); d.Port != 41778 || d.Token != "token-b" {
		t.Fatal(d)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("temp file left behind", entries)
	}
}

func TestDiscoveryWriteTightensLooseExistingModeAndIsAtomicForReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), DiscoveryFile)
	if e := os.WriteFile(path, []byte("stale"), 0o644); e != nil {
		t.Fatal(e)
	}
	if e := WriteDiscovery(path, 1, "t"); e != nil {
		t.Fatal(e)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Fatal("old mode survived", fi.Mode())
		}
	}
	// A reader running during rewrites must always see a complete document.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if runtime.GOOS == "windows" {
				// Go opens files without FILE_SHARE_DELETE, so a rename waits (WriteDiscovery retries for up to
				// 2 s) until the reader's handle is closed; give it gaps instead of a hammering loop.
				time.Sleep(time.Millisecond)
			}
			if raw, e := os.ReadFile(path); e == nil {
				var d Discovery
				if json.Unmarshal(raw, &d) != nil {
					t.Error("torn read", string(raw))
					return
				}
			}
		}
	}()
	for i := 0; i < 200; i++ {
		if e := WriteDiscovery(path, i+1, "t"); e != nil {
			t.Fatal(e)
		}
	}
	close(stop)
	wg.Wait()
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if e := os.Symlink(target, link); e != nil {
		t.Skip("symlinks unavailable:", e)
	}
}

func TestDiscoveryRefusesSymlinkAndNeverWritesThroughIt(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	os.WriteFile(victim, []byte("keep"), 0o600)
	path := filepath.Join(dir, DiscoveryFile)
	symlink(t, victim, path)
	if e := WriteDiscovery(path, 1, "secret"); e == nil {
		t.Fatal("followed or replaced symlink")
	}
	if raw, _ := os.ReadFile(victim); string(raw) != "keep" {
		t.Fatal("wrote through symlink", string(raw))
	}
	if fi, e := os.Lstat(path); e != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was replaced", e)
	}
	if e := RemoveDiscovery(path); e == nil {
		t.Fatal("removed a file that is not ours")
	}
	if _, e := os.Stat(victim); e != nil {
		t.Fatal(e)
	}
	// Dangling symlink: still refused.
	os.Remove(path)
	symlink(t, filepath.Join(dir, "missing"), path)
	if WriteDiscovery(path, 1, "secret") == nil {
		t.Fatal("followed dangling symlink")
	}
	if _, e := os.Stat(filepath.Join(dir, "missing")); e == nil {
		t.Fatal("created the link target")
	}
}

func TestDiscoveryRefusesDirectoryAndRemoveToleratesMissing(t *testing.T) {
	dir := t.TempDir()
	if WriteDiscovery(dir, 1, "t") == nil {
		t.Fatal("overwrote a directory")
	}
	path := filepath.Join(dir, DiscoveryFile)
	if e := RemoveDiscovery(path); e != nil {
		t.Fatal(e)
	}
	WriteDiscovery(path, 1, "t")
	if e := RemoveDiscovery(path); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Lstat(path); !os.IsNotExist(e) {
		t.Fatal("not removed")
	}
}
