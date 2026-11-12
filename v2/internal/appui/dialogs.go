package appui

import (
	"fmt"
	"strings"

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
	title, message, action, cancel string
	danger                         bool
	run                            func()
}

// The confirmations all look and behave alike: the wide dialog, with the focus on the button that backs out,
// so that Enter never does the action. confirm is the plain one, for an action that can be undone;
// confirmDanger fills the action's button red, for one that cannot (deleting, emptying, discarding, sending to
// every session, rotating a token).

// confirm asks before run, in the window that asked.
func (a *App) confirm(title, message, action string, run func()) {
	a.confirmWith(title, message, action, "取消", run)
}

// confirmDanger is confirm for an action that cannot be undone.
func (a *App) confirmDanger(title, message, action string, run func()) {
	a.confirmDangerWith(title, message, action, "取消", run)
}

// confirmWith is confirm with its own name for the button that backs out.
func (a *App) confirmWith(title, message, action, cancel string, run func()) {
	a.ask(&confirmation{title: title, message: message, action: action, cancel: cancel, run: run})
}

// confirmDangerWith is confirmDanger with its own name for the button that backs out.
func (a *App) confirmDangerWith(title, message, action, cancel string, run func()) {
	a.ask(&confirmation{title: title, message: message, action: action, cancel: cancel, danger: true, run: run})
}
func (a *App) ask(p *confirmation) {
	a.dialogs.pending = p
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
	if alert(c, &open, p.title, p.message, backOut(p.cancel), action(p.action, p.danger)) == 1 {
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
	a.editorDialogs(c)
	a.retryDialog(c)
	a.closeDialog(c)
	a.confirmView(c, mainWindow)
	d := &a.dialogs
	if len(d.links) > 0 {
		open := true
		if alert(c, &open, "打开外部链接", "请确认完整地址可信：\n"+d.links[0], backOut("取消"), action("打开链接", false)) == 1 {
			a.report(a.P.OpenURL(d.links[0]), "")
		}
		if !open {
			d.links = d.links[1:]
		}
	}
	if a.ConfirmExit {
		var parts []string
		if n := len(a.Tabs); n > 0 {
			parts = append(parts, fmt.Sprintf("%d 个会话", n))
		}
		if n := a.DirtyDocumentCount(); n > 0 {
			parts = append(parts, fmt.Sprintf("%d 个未保存的文件", n))
		}
		msg := "仍有 " + strings.Join(parts, "、")
		if alert(c, &a.ConfirmExit, "退出 NextShell？", msg+"。退出会断开所有连接并丢弃未保存的修改。", backOut("取消"), action("退出", true)) == 1 {
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
	alert(c, &open, "详情", a.dialogs.detail, backOut("关闭"))
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
			switch alert(c, &open, "确定更新主机指纹？", "只有在你确认服务器重装过系统或更换过密钥时才这样做。", backOut("断开"), action("更新指纹并连接", true)) {
			case 1:
				d.answer([]string{})
			case 0:
				d.answer(nil)
			}
			if !open && len(d.prompts) > 0 && d.prompts[0] == p { // Escape or a click outside: the safe answer
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
	return dialogPanel(c, open, func(ui.Element) { fn() })
}

// alertButton is a button of alert. The one with focus is clicked by Enter: it must be the one that does no harm.
type alertButton struct {
	label         string
	primary, risk bool // filled in the main color, or in the danger color
	focus         bool
}

// backOut is the button that closes an alert without doing anything, and has its focus.
func backOut(label string) alertButton { return alertButton{label: label, focus: true} }

// action is the button that does what the alert asks about.
func action(label string, risk bool) alertButton {
	return alertButton{label: label, primary: !risk, risk: risk}
}

// alert is the app's one look for a question: title and message in a dialog, buttons at the end. It
// returns the index of the button clicked, which closes it (*open turns false), or -1; Escape and a click
// outside close it too, choosing nothing.
func alert(c *ui.Context, open *bool, title, message string, buttons ...alertButton) int {
	chosen := -1
	dialogPanel(c, open, func(panel ui.Element) {
		panel.Role(ui.RoleAlertDialog).Label(title)
		ui.Column(c).Width(420).Gap(12).Children(func() {
			ui.Text(c, title).FontSize(17).Bold().FillWidth()
			if message != "" {
				ui.Text(c, message).FillWidth()
			}
			ui.Row(c).FillWidth().Gap(8).Wrap().Justify(ui.End).Children(func() {
				for i, b := range buttons {
					var e ui.Element
					switch {
					case b.risk:
						e = dangerButton(c, b.label)
					case b.primary:
						e = ui.PrimaryButton(c, b.label)
					default:
						e = ui.Button(c, b.label)
					}
					if b.focus {
						e.AutoFocus()
					}
					if e.Clicked() {
						chosen = i
					}
				}
			})
		})
	})
	if chosen >= 0 {
		*open = false
	}
	return chosen
}

func dialogPanel(c *ui.Context, open *bool, fn func(panel ui.Element)) ui.Element {
	t := c.Theme()
	scrim := ui.RGBA(0, 0, 0, .35)
	if t.Dark {
		scrim = ui.RGBA(0, 0, 0, .6)
	}
	return ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		backdrop.Background(scrim)
		panel.Padding(20).Radius(12).Background(t.Surface).Border(1, t.Border).Shadow(0, 16, 48, 0, ui.RGBA(0, 0, 0, .35))
		fn(panel)
	})
}
