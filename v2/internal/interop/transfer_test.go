//go:build interop

package interop

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"nextshell.dev/v2/internal/transfer"
)

func TestPackedTransferDebian(t *testing.T) {
	for _, v := range []Variant{Debian, Ubuntu} {
		t.Run(v.Name, func(t *testing.T) { packedRoundTrip(t, Shared(t, v)) })
	}
}

func packedRoundTrip(t *testing.T, c *Container) {
	_, s := sftpOn(t, c)
	ctx := context.Background()
	local := filepath.Join(t.TempDir(), "tree")
	makeTree(t, local, 200)
	want := localTree(t, local)

	for _, dest := range []string{"/home/tester/packed", "/home/tester/it's a \"packed\" 目录 $x"} {
		Sh(t, s, "rm -rf ~/packed ~/it*packed*")
		var prog int64
		if err := transfer.UploadPacked(ctx, s, local, dest, func(n int64) { prog = n }); err != nil {
			t.Fatalf("%q: %v", dest, err)
		}
		if got := remoteTree(t, s, dest); !slices.Equal(got, want) {
			t.Fatalf("%q: tree differs", dest)
		}
		t.Logf("%q: %d bytes of tar.gz sent", dest, prog)
		if err := transfer.UploadPacked(ctx, s, local, dest, nil); err == nil {
			t.Errorf("%q: packed upload merged into / replaced an existing destination", dest)
		}
		if got := remoteTree(t, s, dest); !slices.Equal(got, want) {
			t.Errorf("%q: refused upload damaged the existing tree", dest)
		}
		if left := strings.TrimSpace(Sh(t, s, "ls -A ~ | grep nextshell-pack || true")); left != "" {
			t.Errorf("%q: staging left behind: %s", dest, left)
		}

		back := filepath.Join(t.TempDir(), "back")
		if err := transfer.DownloadPacked(ctx, s, dest, back, nil); err != nil {
			t.Fatalf("%q: download: %v", dest, err)
		}
		if got := localTree(t, back); !slices.Equal(got, want) {
			t.Fatalf("%q: downloaded tree differs", dest)
		}
		if err := transfer.DownloadPacked(ctx, s, dest, back, nil); err == nil {
			t.Errorf("download over an existing local dir succeeded")
		}
	}
	if err := transfer.DownloadPacked(ctx, s, "/home/tester/does-not-exist", filepath.Join(t.TempDir(), "x"), nil); err == nil {
		t.Error("download of a missing remote dir succeeded")
	}
}

func TestPackedTransferAlpineBusyBox(t *testing.T) {
	// BusyBox tar and mv understand everything packed transfer uses (tar -xzf/-czf, mv -T -n).
	packedRoundTrip(t, Shared(t, Alpine))
}

// A remote whose mv lacks -T (older BusyBox, BSD/macOS, some appliances): the upload must fail with a
// message that names the problem, leave no destination and no staging directory, and never touch
// anything that already exists.
func TestPackedUploadWithoutGNUMv(t *testing.T) {
	c := Start(t, Spec{Variant: Alpine})
	c.Exec(t, `mv /bin/mv /bin/mv.real && printf '#!/bin/sh\ncase " $* " in *" -T "*) echo "mv: unrecognized option: T" >&2; exit 1;; esac\nexec /bin/mv.real "$@"\n' > /bin/mv && chmod +x /bin/mv`)
	_, s := sftpOn(t, c)
	local := filepath.Join(t.TempDir(), "tree")
	makeTree(t, local, 30)
	Sh(t, s, "mkdir -p ~/keep && echo precious > ~/keep/data")

	err := transfer.UploadPacked(context.Background(), s, local, "/home/tester/new", nil)
	if err == nil {
		t.Fatal("upload succeeded although mv -T is unavailable")
	}
	t.Logf("error shown to the user: %v", err)
	if !strings.Contains(err.Error(), "unrecognized option") {
		t.Errorf("error does not carry the remote's complaint: %q", err)
	}
	if out := strings.TrimSpace(Sh(t, s, "ls -A ~ | grep -E '^new$|nextshell' || true")); out != "" {
		t.Fatalf("failed upload left %q behind", out)
	}
	if got := Sh(t, s, "cat ~/keep/data"); got != "precious\n" {
		t.Fatalf("existing data changed: %q", got)
	}
}
