//go:build interop

package interop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"nextshell.dev/v2/internal/remoteedit"
)

// Open/Save must preserve CRLF, BOM, missing trailing newline, mode and ownership semantics, and detect a
// concurrent remote change (SFTP overwrite goes through posix-rename; the destination is replaced atomically).
func TestRemoteEditCRLFRoundTrip(t *testing.T) {
	eachVariant(t, unixVariants, func(t *testing.T, c *Container) {
		sc, s := sftpOn(t, c)
		ctx := context.Background()
		const orig = "\xef\xbb\xbfline one\r\n中文第二行\r\n\r\nlast line without newline"
		file := "/home/tester/edit-crlf.txt"
		if err := sc.UploadReader(ctx, strings.NewReader(orig), file, true, 0640, nil); err != nil {
			t.Fatal(err)
		}
		doc, err := remoteedit.Open(sc, file)
		if err != nil {
			t.Fatal(err)
		}
		if doc.Text != orig {
			t.Fatalf("Open altered the text: %q", doc.Text)
		}
		edited := strings.Replace(doc.Text, "line one", "LINE ONE", 1) + "\r\nappended\r\n"
		saved, err := remoteedit.Save(ctx, sc, doc, edited)
		if err != nil {
			t.Fatal(err)
		}
		if got := Sh(t, s, "cat "+file); got != edited {
			t.Fatalf("remote bytes %q != %q", got, edited)
		}
		if got := strings.TrimSpace(Sh(t, s, "od -An -c "+file+" | head -3")); !strings.Contains(got, `\r  \n`) {
			t.Errorf("no CRLF pairs on the remote: %s", got)
		}
		if mode := strings.TrimSpace(Sh(t, s, "stat -c %a "+file)); mode != "640" {
			t.Errorf("mode became %s", mode)
		}
		if left := strings.TrimSpace(Sh(t, s, "ls -A ~ | grep nextshell || true")); left != "" {
			t.Errorf("temp files left: %s", left)
		}

		// someone else edits the file between our Open and Save
		Sh(t, s, "printf 'changed by someone else\\n' > "+file)
		if _, err = remoteedit.Save(ctx, sc, saved, "my edit\r\n"); !errors.Is(err, remoteedit.ErrConflict) {
			t.Fatalf("want ErrConflict, got %v", err)
		}
		if got := Sh(t, s, "cat "+file); got != "changed by someone else\n" {
			t.Fatalf("conflicting save damaged the remote file: %q", got)
		}

		// refuse what the editor cannot round-trip
		Sh(t, s, "head -c 1000 /dev/urandom > ~/bin.dat; ln -sf edit-crlf.txt ~/edit-link")
		if _, err = remoteedit.Open(sc, "/home/tester/bin.dat"); err == nil {
			t.Error("opened a binary file")
		}
		if _, err = remoteedit.Open(sc, "/home/tester/edit-link"); err == nil {
			t.Error("opened a symlink (a save would replace the link)")
		}
		if _, err = remoteedit.Open(sc, "/home/tester/missing"); err == nil {
			t.Error("opened a missing file")
		}
	})
}
