package appui_test

import (
	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/appui"
	"testing"
)

type linkPlatform struct {
	*fakePlatform
	opened []string
}

func (p *linkPlatform) OpenURL(s string) error { p.opened = append(p.opened, s); return nil }
func TestTerminalLinksConfirmFullTargetAndCancel(t *testing.T) {
	_, base, _ := fixture(t)
	p := &linkPlatform{fakePlatform: base}
	a := appui.New(p)
	tt := ui.NewTester(a.View, 1400, 1000)
	url := "https://example.invalid/real-target"
	base.events = []appui.Event{{Link: url}}
	tt.Frame()
	if len(p.opened) != 0 || !tt.HasText("链接显示文本可能与实际目标地址不一致，请确认完整地址可信：\n"+url) {
		t.Fatal("opened without confirmation")
	}
	click(t, tt, "取消打开链接")
	if len(p.opened) != 0 {
		t.Fatal("cancel opened")
	}
	base.events = []appui.Event{{Link: url}}
	tt.Frame()
	click(t, tt, "打开链接")
	if len(p.opened) != 1 || p.opened[0] != url {
		t.Fatal(p.opened)
	}
	a.Prefs.HyperlinkConfirm = false
	base.events = []appui.Event{{Link: url}}
	tt.Frame()
	if len(p.opened) != 2 {
		t.Fatal("explicit optout ignored")
	}
}
