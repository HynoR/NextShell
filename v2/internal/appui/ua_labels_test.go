package appui_test

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
)

// The labels name the chord that works with the terminal in focus: ⌘K on macOS, Ctrl+Shift+K elsewhere
// (a plain Ctrl+letter is the shell's); find reads ⌘⇧F or Ctrl+Shift+F, never 「Ctrl+⇧F」; the palette's
// footer names Option on macOS and Alt elsewhere.
func TestShortcutLabelsPerSystem(t *testing.T) {
	for _, v := range []struct {
		goos              string
		app, find, option string
		bad               []string
	}{
		{"darwin", "⌘", "⌘⇧F", "⌥回车", []string{"Ctrl", "Alt+"}},
		{"linux", "Ctrl+Shift+", "Ctrl+Shift+F", "Alt+回车", []string{"⌘", "⌥", "Ctrl+⇧"}},
		{"windows", "Ctrl+Shift+", "Ctrl+Shift+F", "Alt+回车", []string{"⌘", "⌥", "Ctrl+⇧"}},
	} {
		t.Run(v.goos, func(t *testing.T) {
			defer appui.SetOS(v.goos)()
			p := newPlatform(t)
			_, tt := start(p)
			// Welcome page: the title bar's search and the hint under the quick connect.
			has(t, tt, v.app+"K", v.app+"K 搜索主机、标签和操作")
			session(t, p, tt, "A", "web", remoteHost("a"))
			has(t, tt, "查找 "+v.find)
			for label, key := range map[string]string{"收起左栏": "B", "收起工作区": "J"} {
				if info, ok := tt.Access(label); !ok || !strings.Contains(info.Description, v.app+key) {
					t.Fatalf("the tip of %s is %q, want %s%s", label, info.Description, v.app, key)
				}
			}
			tt.Key(appMod(), ui.KeyK)
			has(t, tt, "连接并打开 SFTP · "+v.option+" 片段发送到全部会话", v.app+"B")
			for _, bad := range v.bad {
				for _, s := range tt.Texts() {
					if strings.Contains(s, bad) && !strings.Contains(s, "最近使用") {
						t.Fatalf("%q in %q", bad, s)
					}
				}
			}
		})
	}
}
