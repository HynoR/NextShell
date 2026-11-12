package appui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// cmdbarState is the command bar under the terminal and its find bar.
type cmdbarState struct {
	text, draft string
	at          int // the history entry shown, counted from the newest (1); 0 is the line being typed
	finding     bool
	find        string
	searched    string // the session and the text last searched
}

// commandBar sends a line to the session; ↑/↓ walk the saved history, as a shell does.
func (a *App) commandBar(c *ui.Context, s *Session) {
	t := c.Theme()
	b := &a.cmdbar
	ui.Row(c).FillWidth().Height(36).Padding(0, 8).Gap(6).AlignItems(ui.Center).Background(t.Surface).BorderColor(t.Border).BorderWidth(1, 0, 0, 0).Children(func() {
		ui.Text(c, "$").Font("monospace").FontSize(13).TextColor(t.TextMuted)
		in := ui.TextInput(c.Key("cmdbar"), &b.text).Label("命令输入").Placeholder("输入命令，回车发送，↑/↓ 历史").Font("monospace").FontSize(12).Height(26).Grow(1).MinWidth(0).OnTextKey(func(k ui.TextKey) bool {
			h := a.Data.History
			switch k.Key {
			case ui.KeyEnter:
				a.sendLine(s, k.Text())
				k.SetText("")
				return true
			case ui.KeyUp:
				if b.at == 0 {
					b.draft = k.Text()
				}
				if b.at < len(h) {
					b.at++
					k.SetText(h[len(h)-b.at])
				}
				return true
			case ui.KeyDown:
				if b.at > 0 {
					b.at--
					if b.at == 0 {
						k.SetText(b.draft)
					} else {
						k.SetText(h[len(h)-b.at])
					}
				}
				return true
			}
			return false
		})
		if ghost(c, "发送", "").Disabled(strings.TrimSpace(b.text) == "").Clicked() {
			a.sendLine(s, b.text)
			b.text = ""
			in.Focus()
		}
		find := iconButton(c, "查找 "+shortcutLabel("⇧F"), "search")
		if b.finding {
			find.Background(t.SurfacePressed)
		}
		if find.Clicked() {
			a.toggleFind(s)
		}
	})
}

// sendLine types text and Return into the session and keeps it in the history.
func (a *App) sendLine(s *Session, text string) {
	a.cmdbar.at, a.cmdbar.draft = 0, ""
	if strings.TrimSpace(text) == "" {
		return
	}
	if e := a.P.ExecuteCommand(s.ID, text, false, true); e != nil {
		a.fail(e)
		return
	}
	a.rememberCommand(text)
}

func (a *App) toggleFind(s *Session) {
	b := &a.cmdbar
	b.finding = !b.finding
	if !b.finding {
		s.Terminal.EndSearch()
		b.searched = ""
		a.tabs.term.Focus()
	}
}

// findBar searches the terminal's screen and scrollback as the text is typed; Return goes to the
// next (older) match, Shift+Return to the previous one, Esc closes.
func (a *App) findBar(c *ui.Context, s *Session) {
	t := c.Theme()
	b := &a.cmdbar
	if key := s.ID + "\x00" + b.find; key != b.searched { // a tab switched to is searched too
		b.searched = key
		s.Terminal.Search(b.find)
	}
	st := s.Terminal.SearchStatus()
	bar := ui.Row(c).FillWidth().Height(36).Padding(0, 8).Gap(4).AlignItems(ui.Center).Background(t.Surface).BorderColor(t.Border).BorderWidth(1, 0, 0, 0)
	if bar.Shortcut(0, ui.KeyEscape) {
		a.toggleFind(s)
		return
	}
	bar.Children(func() {
		ui.Icon(c, appIcons["search"]).FontSize(14).TextColor(t.TextMuted)
		ui.TextInput(c.Key("findbar"), &b.find).Label("查找终端内容").Placeholder("查找终端内容").FontSize(12).Height(26).Width(260).AutoFocus().OnTextKey(func(k ui.TextKey) bool {
			switch {
			case k.Key == ui.KeyEnter && k.Mods&ui.Shift != 0:
				s.Terminal.SearchPrevious()
			case k.Key == ui.KeyEnter:
				s.Terminal.SearchNext()
			default:
				return false
			}
			return true
		})
		count := ""
		switch {
		case b.find == "":
		case st.Total == 0 && !st.Searching:
			count = "无结果"
		default:
			count = fmt.Sprintf("%d / %d", st.Selected, st.Total)
		}
		muted(c, count).Label("查找结果 " + count).MinWidth(64)
		if iconButton(c, "上一个", "chevron-up").Clicked() {
			s.Terminal.SearchPrevious()
		}
		if iconButton(c, "下一个", "chevron-down").Clicked() {
			s.Terminal.SearchNext()
		}
		ui.Spacer(c).Grow(1)
		if iconButton(c, "关闭查找", "close").Clicked() {
			a.toggleFind(s)
		}
	})
}
