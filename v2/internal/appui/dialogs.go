package appui

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

type dialogState struct {
	prompts    []*Prompt
	answers    []string
	remember   bool
	changedAsk bool // the second confirmation of a changed host key
	links      []string
	detail     string
	detailIn   window // where the details of an error toast show: the window it showed in
	pending    *confirmation
	pendingIn  window // the window that asked shows it
}

// confirmation is an alert for a dangerous action, whose button names the action.
type confirmation struct {
	title, message, action string
	run                    func()
}

// confirm asks before run, in the window that asked.
func (a *App) confirm(title, message, action string, run func()) {
	a.dialogs.pending = &confirmation{title, message, action, run}
	a.dialogs.pendingIn = a.view
	if a.view == editorWindow { // it has no alerts of its own
		a.dialogs.pendingIn = mainWindow
	}
}
func (a *App) confirmView(c *ui.Context, w window) {
	d := &a.dialogs
	if d.pending == nil || d.pendingIn != w {
		return
	}
	p := d.pending
	open := true
	if ui.AlertDialog(c, &open, p.title, p.message, "取消", p.action) == 1 {
		p.run()
	}
	if !open && d.pending == p {
		d.pending = nil
	}
}

// prunePrompts drops prompts the platform stopped waiting for.
func (d *dialogState) prunePrompts() {
	alive := d.prompts[:0]
	for _, p := range d.prompts {
		select {
		case <-p.Done:
		default:
			alive = append(alive, p)
		}
	}
	if len(alive) == 0 || len(d.prompts) > 0 && alive[0] != d.prompts[0] {
		d.answers, d.remember, d.changedAsk = nil, false, false
	}
	d.prompts = alive
}
func (d *dialogState) answer(values []string) {
	p := d.prompts[0]
	p.Remember = d.remember // written before the send, read after the receive
	select {
	case p.Answers <- values:
	default:
	}
	d.prompts = d.prompts[1:]
	d.answers, d.remember, d.changedAsk = nil, false, false
}

func (a *App) dialogsView(c *ui.Context) {
	a.fileDialogView(c)
	a.snippetDialogs(c)
	a.editorDialog(c)
	a.retryDialog(c)
	a.confirmView(c, mainWindow)
	d := &a.dialogs
	if len(d.links) > 0 {
		open := true
		if ui.AlertDialog(c, &open, "打开外部链接", "请确认完整地址可信：\n"+d.links[0], "取消", "打开链接") == 1 {
			a.report(a.P.OpenURL(d.links[0]), "")
		}
		if !open {
			d.links = d.links[1:]
		}
	}
	if a.ConfirmExit {
		msg := fmt.Sprintf("仍有 %d 个会话", len(a.Tabs))
		if n := a.DirtyDocumentCount(); n > 0 {
			msg += fmt.Sprintf("、%d 个未保存的文件", n)
		}
		if ui.AlertDialog(c, &a.ConfirmExit, "退出 NextShell？", msg+"。退出会断开所有连接并丢弃未保存的修改。", "取消", "退出") == 1 {
			a.ExitConfirmed = true
			a.P.Quit()
		}
	}
	a.detailView(c, mainWindow)
	if len(d.prompts) > 0 {
		a.promptView(c, d.prompts[0])
	}
}
func (a *App) detailView(c *ui.Context, w window) {
	if a.dialogs.detail == "" || a.dialogs.detailIn != w {
		return
	}
	open := true
	ui.AlertDialog(c, &open, "详情", a.dialogs.detail, "关闭")
	if !open {
		a.dialogs.detail = ""
	}
}

// agentOrigin tells the user that a connection dialog is not about something they just did.
const agentOrigin = "由 Agent 发起"

func (a *App) promptView(c *ui.Context, p *Prompt) {
	d := &a.dialogs
	t := c.Theme()
	switch p.Kind {
	case PromptHostKey:
		open := true
		dialog(c, &open, func() {
			ui.Column(c).Width(520).Gap(12).Children(func() {
				ui.Text(c, "首次连接，请核对主机指纹").FontSize(17).Bold()
				if p.ByAgent {
					muted(c, agentOrigin)
				}
				ui.Text(c, "请与服务器管理员提供的指纹核对，确认无误后再信任。").FillWidth()
				ui.Column(c).FillWidth().Padding(12).Gap(6).Radius(t.Radius).Background(t.Background).Border(1, t.Border).Children(func() {
					muted(c, "主机 "+p.Host)
					muted(c, "指纹")
					ui.Text(c, p.Fingerprint).Font("monospace").FontSize(12).Selectable()
				})
				ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c, "取消").Clicked() {
						d.answer(nil)
					}
					if ui.PrimaryButton(c, "信任并连接").AutoFocus().Clicked() {
						d.answer([]string{})
					}
				})
			})
		})
		if !open {
			d.answer(nil)
		}
	case PromptHostKeyChanged:
		if d.changedAsk {
			open := true
			switch ui.AlertDialog(c, &open, "确定更新主机指纹？", "只有在你确认服务器重装过系统或更换过密钥时才这样做。", "断开", "更新指纹并连接") {
			case 1:
				d.answer([]string{})
			case ui.AlertDismissed, 0:
				d.answer(nil)
			}
			return
		}
		open := true
		dialog(c, &open, func() {
			ui.Column(c).Width(520).Gap(12).Children(func() {
				ui.Text(c, "主机指纹已变化").FontSize(17).Bold().TextColor(t.Danger)
				if p.ByAgent {
					muted(c, agentOrigin)
				}
				ui.Text(c, "服务器的身份与上次不同。这可能是服务器重装过，也可能有人在拦截连接。在确认原因之前，请不要继续。").FillWidth()
				ui.Column(c).FillWidth().Padding(12).Gap(6).Radius(t.Radius).Background(t.Danger.Alpha(.08)).Border(1, t.Danger.Alpha(.4)).Children(func() {
					muted(c, "主机 "+p.Host)
					muted(c, "已保存的指纹")
					ui.Text(c, p.OldFingerprint).Font("monospace").FontSize(12).Selectable()
					muted(c, "现在的指纹")
					ui.Text(c, p.Fingerprint).Font("monospace").FontSize(12).Selectable().TextColor(t.Danger)
				})
				ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c, "更新指纹并连接…").Clicked() {
						d.changedAsk = true
					}
					if ui.PrimaryButton(c, "断开").AutoFocus().Clicked() {
						d.answer(nil)
					}
				})
			})
		})
		if !open {
			d.answer(nil)
		}
	default:
		if len(d.answers) != len(p.Questions) {
			d.answers = make([]string, len(p.Questions))
		}
		open := true
		dialog(c, &open, func() {
			ui.Column(c).Width(420).Gap(12).Children(func() {
				ui.Text(c, p.Title).FontSize(17).Bold()
				if p.ByAgent {
					muted(c, agentOrigin)
				}
				if p.Host != "" {
					muted(c, p.Host)
				}
				submitted := false
				ui.Form(c, func() {
					for i, q := range p.Questions {
						in := ui.TextInput(c.Key(fmt.Sprint("answer", i)), &d.answers[i]).Label(q)
						if i >= len(p.Echo) || !p.Echo[i] {
							in.Password()
						}
						if i == 0 {
							in.AutoFocus()
						}
						if in.Submitted() && i == len(p.Questions)-1 {
							submitted = true
						}
					}
				}).FillWidth().Gap(FormGap)
				if p.CanRemember {
					ui.Checkbox(c, &d.remember, "记住密码")
				}
				ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c, "取消").Clicked() {
						d.answer(nil)
					}
					label := "继续"
					if p.Kind == PromptPassword {
						label = "连接"
					}
					if ui.PrimaryButton(c, label).Clicked() || submitted {
						d.answer(append([]string{}, d.answers...))
					}
				})
			})
		})
		if !open && len(d.prompts) > 0 && d.prompts[0] == p {
			d.answer(nil)
		}
	}
}

// dialog is ui.Modal with the app's look: a dimming scrim, and a panel one
// step lighter than the window with a border, so it stands out in dark mode too.
func dialog(c *ui.Context, open *bool, fn func()) ui.Element {
	t := c.Theme()
	scrim := ui.RGBA(0, 0, 0, .35)
	if t.Dark {
		scrim = ui.RGBA(0, 0, 0, .6)
	}
	return ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		backdrop.Background(scrim)
		panel.Padding(20).Radius(12).Background(t.Surface).Border(1, t.Border).Shadow(0, 16, 48, 0, ui.RGBA(0, 0, 0, .35))
		fn()
	})
}
