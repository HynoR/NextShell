//go:build interop

package interop

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"nextshell.dev/v2/internal/sshclient"
)

func shell(t *testing.T, c *Container, mutate ...func(*sshclient.Options)) (*Term, *sshclient.Session) {
	t.Helper()
	o := c.Options()
	for _, m := range mutate {
		m(&o)
	}
	s := Dial(t, o)
	x := NewTerm(t, s)
	x.Sync(t, 10*time.Second)
	return x, s
}

func TestPTYShellBasics(t *testing.T) {
	c := Shared(t, Debian)
	x, s := shell(t, c)

	x.Send("echo TERM=$TERM\n")
	x.Expect(t, "TERM=xterm-256color", 5*time.Second)

	// the client's initial PTY is 80x24; Resize must reach the remote kernel
	x.Send("stty size\n")
	x.Expect(t, "24 80", 5*time.Second)
	if err := s.Resize(132, 43); err != nil {
		t.Fatal(err)
	}
	x.Send("stty size\n")
	x.Expect(t, "43 132", 5*time.Second)

	// UTF-8: remote computes the digit, so the echoed command line cannot satisfy the match
	x.Send("echo 中文测试$((1+1))éñ😀\n")
	x.Expect(t, "中文测试2éñ😀", 5*time.Second)
	x.Send("printf '%s\\n' \"$(printf '\\344\\270\\255')文件\"\n")
	x.Expect(t, "中文件", 5*time.Second)

	// control keys: ^C kills a foreground job, ^D on an empty line exits
	x.Send("sleep 300\n")
	time.Sleep(300 * time.Millisecond)
	x.Send("\x03")
	x.Sync(t, 5*time.Second)
	x.Send("\x04")
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session still open after ^D")
	}
	if err := s.WaitError(); err != nil {
		t.Fatalf("clean exit reported %v", err)
	}
}

func TestPTYFullScreenPrograms(t *testing.T) {
	c := Shared(t, Debian)

	t.Run("vim", func(t *testing.T) {
		x, s := shell(t, c)
		x.Send("vim -u NONE -N -n /tmp/vim-out.txt\n")
		x.Expect(t, "~", 10*time.Second) // empty-buffer filler lines
		time.Sleep(300 * time.Millisecond)
		x.Send("ihello 世界\x1b")            // insert mode, text, <Esc>
		time.Sleep(500 * time.Millisecond) // a quick ':' after <Esc> would read as <M-:>
		if err := s.Resize(100, 30); err != nil {
			t.Fatal(err)
		}
		time.Sleep(500 * time.Millisecond) // let vim handle SIGWINCH first
		x.Send(":echo &columns . 'x' . &lines\r")
		x.Expect(t, "100x30", 5*time.Second) // vim noticed SIGWINCH
		x.Send(":wq\r")
		x.Sync(t, 10*time.Second)
		if got := Sh(t, s, "cat /tmp/vim-out.txt"); got != "hello 世界\n" {
			t.Fatalf("vim wrote %q", got)
		}
	})

	t.Run("htop", func(t *testing.T) {
		x, s := shell(t, c)
		x.Send("htop\n")
		x.Expect(t, "Tasks", 15*time.Second)
		for _, size := range [][2]int{{120, 40}, {60, 20}, {200, 60}} { // htop redraws on every resize
			if err := s.Resize(size[0], size[1]); err != nil {
				t.Fatal(err)
			}
			time.Sleep(300 * time.Millisecond)
		}
		x.Expect(t, "Load average", 5*time.Second)
		x.Send("q")
		x.Sync(t, 10*time.Second)
	})

	t.Run("tmux", func(t *testing.T) {
		x, s := shell(t, c)
		x.Send("tmux new-session -s nsi\n")
		x.Expect(t, "[nsi]", 10*time.Second)
		time.Sleep(300 * time.Millisecond)
		x.Send("echo TMUX$((40+2))\n")
		x.Expect(t, "TMUX42", 5*time.Second)
		if err := s.Resize(90, 28); err != nil {
			t.Fatal(err)
		}
		time.Sleep(300 * time.Millisecond)
		x.Send("tmux display-message -p 'SIZE#{client_width}x#{client_height}'\n")
		x.Expect(t, "SIZE90x28", 5*time.Second)
		x.Send("\x02d") // prefix + detach
		x.Expect(t, "detached", 5*time.Second)
		x.Send("tmux kill-server\n")
		x.Sync(t, 10*time.Second)
	})

	t.Run("zsh", func(t *testing.T) {
		x, _ := shell(t, c)
		x.Send("zsh\n")
		time.Sleep(500 * time.Millisecond)
		x.Send("echo ZSH$((40+2)) $ZSH_VERSION\n")
		x.Expect(t, "ZSH42", 10*time.Second)
		x.Send("exit\n")
		x.Sync(t, 10*time.Second)
	})

	t.Run("fish", func(t *testing.T) {
		x, _ := shell(t, c)
		start := time.Now()
		x.Send("fish\n")
		// no terminal emulator answers fish's capability queries; it must still start
		x.Send("echo FISH(math 40+2)\n")
		x.Expect(t, "FISH42", 20*time.Second)
		t.Logf("fish usable after %v", time.Since(start).Round(10*time.Millisecond))
		x.Send("exit\n")
		x.Sync(t, 10*time.Second)
	})
}

func TestPTYAlpineAsh(t *testing.T) {
	x, _ := shell(t, Shared(t, Alpine))
	x.Send("echo ASH$((40+2)) $LANG; stty size\n")
	x.Expect(t, "ASH42 C.UTF-8", 5*time.Second)
	x.Expect(t, "24 80", 5*time.Second)
	x.Send("echo 中文$((1+1))\n")
	x.Expect(t, "中文2", 5*time.Second)
}

func TestGBKEncoding(t *testing.T) {
	c := Shared(t, Debian)
	x, _ := shell(t, c, func(o *sshclient.Options) { o.Connection.Encoding = "gbk" })

	// remote -> client: GBK bytes for 你好 must come out as UTF-8 text
	x.Send("printf '\\304\\343\\272\\303\\n'\n")
	x.Expect(t, "你好", 5*time.Second)

	// client -> remote: typed 你好 must leave as GBK bytes (c4 e3 ba c3)
	x.Send("printf 你好 | od -An -tx1\n")
	x.Expect(t, "c4 e3 ba c3", 5*time.Second)

	// large stream: multibyte sequences get split across SSH packets / reads
	const lines = 50000
	x.Send(fmt.Sprintf("yes \"$(printf '\\304\\343\\272\\303 \\326\\320\\316\\304')\" | head -n %d; echo END$((1+1))\n", lines))
	body := x.Expect(t, "END2", 30*time.Second)
	if n := strings.Count(body, "你好 中文"); n != lines {
		t.Fatalf("decoded %d of %d GBK lines", n, lines)
	}
	if strings.ContainsRune(body, '�') {
		t.Fatal("replacement characters in decoded GBK stream")
	}
}
