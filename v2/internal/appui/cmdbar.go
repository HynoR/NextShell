package appui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/commandtemplate"
	"nextshell.dev/v2/internal/model"
)

// cmdbarState is the command bar under the terminal and its find bar.
type cmdbarState struct {
	text, draft string
	owner       string              // the session the text is for: a line typed for one tab is never sent to another
	drafts      map[string]cmdDraft // the lines waiting in the other tabs, until sent or the tab closes
	at          int                 // the history entry shown, counted from the newest (1); 0 is the line being typed
	finding     bool
	find        string
	searched    string         // the session and the text last searched
	snippet     *model.Command // the snippet last put in the bar, until a line is sent
	focus       bool           // the next frame moves the keyboard focus into the bar
}

// cmdDraft is what the command bar held for a tab it is not showing.
type cmdDraft struct {
	text    string
	snippet *model.Command
}

// switchTo makes the bar's text the one of session id: the line left in the bar goes back to its own tab's
// draft and id's comes in, so a command typed for one server is not sent to the next tab's.
func (b *cmdbarState) switchTo(id string) {
	if b.owner == id {
		return
	}
	if b.drafts == nil {
		b.drafts = map[string]cmdDraft{}
	}
	if b.owner != "" && b.text != "" {
		b.drafts[b.owner] = cmdDraft{b.text, b.snippet}
	}
	d := b.drafts[id]
	delete(b.drafts, id)
	b.owner, b.text, b.snippet = id, d.text, d.snippet
	b.at, b.draft = 0, ""
}

// forget drops what the bar holds for a closed session.
func (b *cmdbarState) forget(id string) {
	delete(b.drafts, id)
	if b.owner == id {
		b.owner, b.text, b.snippet = "", "", nil
		b.at, b.draft = 0, ""
	}
}

// rehome gives a reconnected session's line to its replacement.
func (b *cmdbarState) rehome(old, next string) {
	if b.owner == old {
		b.owner = next
	}
	if d, ok := b.drafts[old]; ok {
		delete(b.drafts, old)
		b.drafts[next] = d
	}
}

// commandBar sends a line to the session; ↑/↓ walk the saved history, as a shell does. It names its
// target, so there is no doubt which server a line goes to.
func (a *App) commandBar(c *ui.Context, s *Session) {
	t := c.Theme()
	b := &a.cmdbar
	b.switchTo(s.ID)
	title := a.tabTitle(s)
	exited := a.P.Exited(s.ID)
	status, _ := a.sessionStatus(c, s)
	placeholder := "输入命令，回车发送，↑/↓ 历史"
	switch {
	case status == "连接中":
		placeholder = "正在重新连接…"
	case exited && s.Local:
		placeholder = "终端已退出，回车重新打开"
	case exited:
		placeholder = "连接已断开，回车重新连接"
	}
	ui.Row(c).FillWidth().Height(36).Padding(0, 8).Gap(6).AlignItems(ui.Center).Background(a.pane(t)).BorderColor(t.Border).BorderWidth(1, 0, 0, 0).Children(func() {
		if !s.Local {
			ui.Box(c).Size(3, 14).Radius(1.5).Background(a.hostColor(s.Connection))
		}
		ui.Text(c, "→ "+title).FontSize(12).TextColor(t.TextMuted).SingleLine().MaxWidth(160).Shrink(1).Tooltip("命令发送到 " + title)
		ui.Text(c, "$").Font("monospace").FontSize(13).TextColor(t.TextMuted)
		in := ui.TextInput(c.Key("cmdbar"), &b.text).Label("命令输入").Placeholder(placeholder).Font("monospace").FontSize(12).Height(26).Grow(1).MinWidth(0).OnTextKey(func(k ui.TextKey) bool {
			h := a.Data.History
			switch k.Key {
			case ui.KeyEscape:
				a.tabs.term.Focus()
				return true
			case ui.KeyEnter:
				if exited { // nothing to send to: Return brings the session back
					if status != "连接中" {
						a.reconnect(s)
					}
					return true
				}
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
		if b.focus {
			b.focus = false
			in.Focus()
		}
		if ghost(c, "发送", "").Disabled(exited || strings.TrimSpace(b.text) == "").Clicked() {
			a.sendLine(s, b.text)
			b.text = ""
			in.Focus()
		}
		find := iconButton(c, "查找 "+findShortcutLabel(), "search")
		if b.finding {
			find.Background(t.SurfacePressed)
		}
		if find.Clicked() {
			a.toggleFind(s)
		}
	})
}

// fillCommandBar puts a snippet in the bar to be read, edited and sent from there. A snippet of several
// lines does not fit the bar: it waits for its send button.
func (a *App) fillCommandBar(v model.Command) {
	b := &a.cmdbar
	if strings.Contains(strings.TrimSpace(v.Text), "\n") {
		a.toast("多行片段请点 ▶ 发送")
		return
	}
	if id := a.activeID(); id != "" { // the line is for the tab showing
		b.switchTo(id)
	}
	b.text, b.at, b.draft, b.snippet, b.focus = v.Text, 0, "", &v, true
}

// sendLine types text and Return into the session and keeps it in the history. A snippet with parameters
// first asks for them, whatever was changed around them.
func (a *App) sendLine(s *Session, text string) {
	b := &a.cmdbar
	b.at, b.draft = 0, ""
	if strings.TrimSpace(text) == "" {
		return
	}
	if v := b.snippet; v != nil {
		b.snippet = nil
		if len(commandtemplate.Keys(text)) > 0 {
			v.Text = text
			a.runSnippet(*v, false)
			return
		}
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
	bar := ui.Row(c).FillWidth().Height(36).Padding(0, 8).Gap(4).AlignItems(ui.Center).Background(a.pane(t)).BorderColor(t.Border).BorderWidth(1, 0, 0, 0)
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
