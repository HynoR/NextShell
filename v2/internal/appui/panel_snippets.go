package appui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"nextshell.dev/v2/internal/commandtemplate"
	"nextshell.dev/v2/internal/model"
)

type snippetsState struct {
	search, group string
	selected      string    // the snippet clicked, whose text is in the command bar
	doubled       time.Time // when a row was double clicked
	// renaming is the group whose header is an inline text field.
	renaming, renameTo string

	// editor is the 新建 / 编辑片段 dialog.
	editing bool
	draft   model.Command

	// params fills a snippet's [#参数] before sending it.
	paramsOpen, paramsAll bool
	params                []commandParameter
	paramsFor             model.Command

	// command is the free text 发送命令 dialog with the send history.
	commandOpen bool
	command     string
}

type commandParameter struct{ Key, Value string }

func (s *snippetsState) init() { s.group = "全部" }

func commandMatches(v model.Command, q, group string) bool {
	return (group == "全部" || v.Group == group) && strings.Contains(strings.ToLower(v.Name+" "+v.Group+" "+v.Description+" "+v.Text), strings.ToLower(strings.TrimSpace(q)))
}
func appendCR(v model.Command) bool { return v.AppendCR == nil || *v.AppendCR }

// runSnippet sends v to the active session, or to all after a confirmation,
// asking for its parameters first when it has any.
func (a *App) runSnippet(v model.Command, all bool) {
	if keys := commandtemplate.Keys(v.Text); len(keys) > 0 {
		s := &a.snippets
		old := map[string]string{}
		if s.paramsFor.ID == v.ID {
			for _, p := range s.params {
				old[p.Key] = p.Value
			}
		}
		s.params = nil
		for _, k := range keys {
			s.params = append(s.params, commandParameter{k, old[k]})
		}
		s.paramsFor, s.paramsAll, s.paramsOpen = v, all, true
		return
	}
	a.sendText(v.Text, all, appendCR(v))
}

// sendText sends text to the active session or, after a confirmation, to all of them.
func (a *App) sendText(text string, all, cr bool) {
	if all {
		a.sendToAll(text, cr)
		return
	}
	if a.current() == nil {
		a.toast("请先打开一个会话")
		return
	}
	if e := a.P.ExecuteCommand(a.activeID(), text, false, cr); e != nil {
		a.fail(e)
		return
	}
	a.tabs.term.Focus()
}

// sendToAll asks, naming every tab the text goes to, then sends it to those tabs alone: a tab opened
// meanwhile is not one the user agreed to. Disconnected tabs are left out and counted.
func (a *App) sendToAll(text string, cr bool) {
	var targets []*Session
	var names []string
	for _, s := range a.Tabs {
		if a.P.Exited(s.ID) {
			continue
		}
		name := a.tabTitle(s)
		if s.ByAgent {
			name += "（Agent 打开）"
		}
		targets, names = append(targets, s), append(names, name)
	}
	if len(targets) == 0 {
		a.toast("没有可发送的会话：所有会话都已断开")
		return
	}
	msg := "将向以下会话发送：\n" + listNames(names, 10) + "\n\n" + text
	if skipped := len(a.Tabs) - len(targets); skipped > 0 {
		msg += fmt.Sprintf("\n\n已跳过 %d 个已断开的会话。", skipped)
	}
	titles := make([]string, len(targets))
	for i, s := range targets {
		titles[i] = a.tabTitle(s)
	}
	a.confirm("发送到全部会话", msg, fmt.Sprintf("发送到 %d 个会话", len(targets)), func() { // danger
		var failed, detail []string
		for i, s := range targets {
			if e := a.P.ExecuteCommand(s.ID, text, false, cr); e != nil {
				failed, detail = append(failed, titles[i]), append(detail, titles[i]+"："+e.Error())
			}
		}
		if len(failed) == 0 {
			a.toast(fmt.Sprintf("已发送到 %d 个会话", len(targets)))
			return
		}
		a.failAs(fmt.Sprintf("%d 个会话没有收到：%s", len(failed), strings.Join(failed, "、")), strings.Join(detail, "\n"))
	})
}

// listNames puts one name to a line, the first max of them.
func listNames(names []string, max int) string {
	shown := "· " + strings.Join(names[:min(len(names), max)], "\n· ")
	if len(names) > max {
		shown += fmt.Sprintf("\n… 另有 %d 个", len(names)-max)
	}
	return shown
}

func (a *App) snippetsPanel(c *ui.Context) {
	t := c.Theme()
	s := &a.snippets
	groups := []string{"全部"}
	for _, v := range a.Data.Commands {
		if !contains(groups, v.Group) {
			groups = append(groups, v.Group)
		}
	}
	ui.Row(c).FillWidth().Padding(8, 8, 4, 8).Gap(6).Children(func() {
		ui.SearchField(c, &s.search).Label("搜索片段").Placeholder("搜索片段").Grow(1).MinWidth(0)
		if len(groups) > 2 {
			ui.Select(c, &s.group, groups).Label("片段分组").Width(100)
		}
	})
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Padding(4, 8).Gap(2).Children(func() {
			if len(a.Data.Commands) == 0 {
				emptyState(c, "还没有命令片段", "把常用命令存成片段，单击填入命令输入栏，双击发送到当前会话。")
				return
			}
			visible := 0
			last := ""
			for _, v := range a.Data.Commands {
				if !commandMatches(v, s.search, s.group) {
					continue
				}
				if visible == 0 || v.Group != last {
					a.snippetGroupHeader(c, v.Group)
					last = v.Group
				}
				visible++
				a.snippetRow(c, v)
			}
			if visible == 0 {
				muted(c, "没有匹配的片段").Padding(8)
			}
		})
	})
	ui.Row(c).FillWidth().Padding(6, 8).Gap(6).BorderColor(t.Border).BorderWidth(1, 0, 0, 0).Children(func() {
		if ghost(c, "新建片段", "plus").Clicked() {
			a.editSnippet(model.Command{Group: "默认"})
		}
		ui.Spacer(c)
		if ghost(c, "发送命令…", "terminal").Clicked() {
			a.snippets.commandOpen = true
		}
	})
}

// snippetRow is a snippet of the list. A click selects it and fills the command bar, which sends nothing;
// the ▶ button on the row under the pointer (or the selected one) and a double click send it, to the
// session whose name the button's tooltip says.
func (a *App) snippetRow(c *ui.Context, v model.Command) {
	t := c.Theme()
	s := &a.snippets
	selected := s.selected == v.ID
	row := ui.ButtonBase(c.Key(v.ID)).Label("片段 "+v.Name).FillWidth().Padding(6, 6, 6, 8).Gap(6).AlignItems(ui.Center).Radius(t.Radius).Tooltip(v.Text)
	switch {
	case selected:
		row.Background(t.Accent.Alpha(.15))
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}
	a.snippetMenu(row, v)
	var send ui.Element
	row.Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).AlignItems(ui.Start).Children(func() {
			ui.Text(c, v.Name).SingleLine()
			ui.Text(c, firstLine(v.Text)).Font("monospace").FontSize(11).TextColor(t.TextMuted).SingleLine()
		})
		if selected || row.Hovered() {
			tip := "请先打开一个会话"
			if cur := a.current(); cur != nil {
				tip = "发送到 " + a.tabTitle(cur)
			}
			send = ui.ButtonBase(c.Key("send-"+v.ID)).Label("发送 "+v.Name).Tooltip(tip).Size(26, 26).Radius(t.Radius).Center().TextColor(accentInk(t)).Disabled(a.current() == nil)
			if send.Hovered() {
				send.Background(t.SurfaceHover)
			}
			send.Children(func() { ui.Icon(c, appIcons["play"]).FontSize(14) })
		}
	})
	switch {
	case row.DoubleClicked():
		s.doubled = a.now() // the release of this click comes in a frame of its own
		a.later(func() { a.sendSnippet(v) })
	case send.Clicked():
		a.later(func() { a.sendSnippet(v) })
	case row.Clicked() && a.now().Sub(s.doubled) > 400*time.Millisecond:
		a.later(func() {
			s.selected = v.ID
			a.fillCommandBar(v)
		})
	}
}

// sendSnippet sends v to the active session, and takes it out of the command bar, where it waited.
func (a *App) sendSnippet(v model.Command) {
	if b := &a.cmdbar; b.snippet != nil && b.snippet.ID == v.ID && b.text == v.Text {
		b.text, b.snippet = "", nil
	}
	a.snippets.selected = v.ID
	a.runSnippet(v, false)
}
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func (a *App) snippetMenu(e ui.Element, v model.Command) {
	e.ContextMenu(func(m *ui.Menu) {
		if m.Item("发送到当前会话").Chosen() {
			a.later(func() { a.runSnippet(v, false) })
		}
		if m.Item("发送到全部会话…").Chosen() {
			a.later(func() { a.runSnippet(v, true) })
		}
		m.Separator()
		if m.Item("编辑…").Chosen() {
			a.later(func() { a.editSnippet(v) })
		}
		if m.Item("复制文本").Chosen() {
			a.P.CopyText(v.Text)
		}
		if m.Item("另存为副本…").Chosen() {
			a.later(func() {
				v.ID = ""
				v.Name += " 副本"
				a.editSnippet(v)
			})
		}
		m.Separator()
		if m.Item("删除").Danger().Chosen() {
			a.later(func() {
				a.confirm("删除片段「"+v.Name+"」", "删除后无法恢复。", "删除片段", func() { // danger
					a.report(a.P.Delete("command", v.ID), "已删除片段")
					a.reload()
				})
			})
		}
	})
}

func (a *App) editSnippet(v model.Command) {
	a.snippets.draft = v
	a.snippets.editing = true
}
func (a *App) snippetDialogs(c *ui.Context) {
	s := &a.snippets
	if s.editing {
		dialog(c, &s.editing, func() {
			ui.Column(c).Width(520).Gap(16).Children(func() {
				title := "新建片段"
				if s.draft.ID != "" {
					title = "编辑片段"
				}
				ui.Text(c, title).FontSize(17).Bold()
				ui.Form(c, func() {
					ui.Field(c, "名称", func() { ui.TextInput(c, &s.draft.Name).Label("片段名称").AutoFocus().Grow(1) }).FillWidth()
					field(c, "分组", &s.draft.Group)
					field(c, "说明", &s.draft.Description)
					ui.Field(c, "命令", func() { ui.TextArea(c, &s.draft.Text).Label("命令").Font("monospace").Height(110).Grow(1) }).Description("用 [#名称] 标记发送前要填写的参数；片段以明文保存在本机").FillWidth()
					cr := appendCR(s.draft)
					toggle(c, "发送后按回车", &cr)
					s.draft.AppendCR = &cr
				}).FillWidth().Gap(FormGap)
				ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c, "取消").Clicked() {
						s.editing = false
					}
					if ui.PrimaryButton(c, "保存片段").Disabled(strings.TrimSpace(s.draft.Name) == "" || strings.TrimSpace(s.draft.Text) == "").Clicked() {
						v := s.draft
						if v.ID == "" {
							v.ID = a.P.NewID()
						}
						if e := a.P.SaveCommand(v); e != nil {
							a.fail(e)
						} else {
							s.editing = false
							a.reload()
						}
					}
				})
			})
		})
	}
	if s.paramsOpen {
		dialog(c, &s.paramsOpen, func() {
			ui.Column(c).Width(460).Gap(16).Children(func() {
				ui.Text(c, s.paramsFor.Name).FontSize(17).Bold()
				ui.Form(c, func() {
					for i := range s.params {
						in := ui.TextInput(c.Key("param-"+s.params[i].Key), &s.params[i].Value).Label(s.params[i].Key)
						if i == 0 {
							in.AutoFocus()
						}
					}
				}).FillWidth().Gap(FormGap)
				text := s.render()
				ui.Text(c, text).Font("monospace").FontSize(12).Padding(8).FillWidth().Radius(6).Background(c.Theme().Background).Selectable()
				muted(c, "参数原样替换，不做 shell 转义")
				ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c, "取消").Clicked() {
						s.paramsOpen = false
					}
					label := "发送"
					if s.paramsAll {
						label = "发送到全部会话…"
					}
					if ui.PrimaryButton(c, label).Clicked() {
						s.paramsOpen = false
						a.sendText(text, s.paramsAll, appendCR(s.paramsFor))
					}
				})
			})
		})
	}
	if s.commandOpen {
		a.commandDialog(c)
	}
}
func (s *snippetsState) render() string {
	params := map[string]string{}
	for _, v := range s.params {
		params[v.Key] = v.Value
	}
	return commandtemplate.Render(s.paramsFor.Text, params)
}

// commandDialog sends typed commands and keeps their history.
func (a *App) commandDialog(c *ui.Context) {
	s := &a.snippets
	t := c.Theme()
	dialog(c, &s.commandOpen, func() {
		ui.Column(c).Width(560).Gap(12).Children(func() {
			ui.Text(c, "发送命令").FontSize(17).Bold()
			ui.TextInput(c, &s.command).Label("命令输入").Placeholder("回车发送到当前会话").Font("monospace").FillWidth().AutoFocus().OnTextKey(func(k ui.TextKey) bool {
				if text := k.Text(); k.Key == ui.KeyEnter && strings.TrimSpace(text) != "" {
					k.SetText("")
					a.sendCommand(text, false)
					return true
				}
				return false
			})
			muted(c, "历史以明文保存在本机，请勿输入密码")
			if len(a.Data.History) > 0 {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					muted(c, "最近发送").Grow(1)
					if ghost(c, "清空历史", "").Clicked() {
						a.confirm("清空命令历史", "删除全部本地历史，无法恢复。", "清空历史", func() { // danger
							a.report(a.P.SaveHistory(nil), "")
							a.reload()
						})
					}
				})
				ui.Scroll(c).MaxHeight(200).FillWidth().Children(func() {
					ui.Column(c).FillWidth().Gap(2).Children(func() {
						for i := len(a.Data.History) - 1; i >= 0; i-- {
							h := a.Data.History[i]
							row := ui.ButtonBase(c.Key(fmt.Sprint("history-", i))).Label(h).FillWidth().Padding(4, 8).Radius(6)
							if row.Hovered() {
								row.Background(t.SurfaceHover)
							}
							index := i
							row.ContextMenu(func(m *ui.Menu) {
								if m.Item("存为片段…").Chosen() {
									a.later(func() {
										s.commandOpen = false
										a.editSnippet(model.Command{Group: "默认", Name: firstLine(h), Text: h})
									})
								}
								if m.Item("删除").Chosen() {
									a.later(func() {
										next := append(append([]string{}, a.Data.History[:index]...), a.Data.History[index+1:]...)
										a.report(a.P.SaveHistory(next), "")
										a.reload()
									})
								}
							})
							row.Children(func() { ui.Text(c, h).Font("monospace").FontSize(12).SingleLine() })
							if row.Clicked() {
								s.command = h
							}
						}
					})
				})
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				empty := strings.TrimSpace(s.command) == ""
				if ui.Button(c, "发送到全部会话…").Disabled(empty || len(a.Tabs) == 0).Clicked() {
					a.sendCommand(s.command, true)
				}
				if ui.PrimaryButton(c, "发送").Disabled(empty || a.current() == nil).Clicked() {
					a.sendCommand(s.command, false)
				}
			})
		})
	})
}
func (a *App) sendCommand(text string, all bool) {
	a.sendText(text, all, true)
	a.rememberCommand(text)
	a.snippets.command = ""
	if !all {
		a.snippets.commandOpen = false
	}
}

// rememberCommand adds text to the saved command history, the newest last; a repeat moves to the end.
func (a *App) rememberCommand(text string) {
	history := append(slices.DeleteFunc(slices.Clone(a.Data.History), func(h string) bool { return h == text }), text)
	if len(history) > 100 {
		history = history[len(history)-100:]
	}
	a.report(a.P.SaveHistory(history), "")
	a.reload()
}

// snippetGroupHeader names a group; its menu renames or deletes the whole group.
func (a *App) snippetGroupHeader(c *ui.Context, group string) {
	s := &a.snippets
	if s.renaming == group {
		in := ui.TextInput(c.Key("group-rename"), &s.renameTo).Label("分组名称").AutoFocus().FillWidth().Height(24).FontSize(12).Margin(6, 0, 2, 0).OnTextKey(func(k ui.TextKey) bool {
			if k.Key == ui.KeyEscape {
				s.renaming = ""
				return true
			}
			return false
		})
		if in.Submitted() {
			a.renameSnippetGroup(group, strings.TrimSpace(s.renameTo))
		}
		return
	}
	n := 0
	for _, v := range a.Data.Commands {
		if v.Group == group {
			n++
		}
	}
	muted(c, group).Padding(8, 6, 2, 6).Label("分组 " + group).ContextMenu(func(m *ui.Menu) {
		if m.Item("重命名分组…").Chosen() {
			s.renaming, s.renameTo = group, group
		}
		if m.Item("删除分组…").Danger().Chosen() {
			a.later(func() {
				a.confirm(fmt.Sprintf("删除分组「%s」", group), fmt.Sprintf("分组里的 %d 个片段会一起删除，无法恢复。", n), fmt.Sprintf("删除 %d 个片段", n), func() { // danger
					for _, v := range a.Data.Commands {
						if v.Group == group {
							if e := a.P.Delete("command", v.ID); e != nil {
								a.fail(e)
								break
							}
						}
					}
					a.reload()
					s.group = "全部"
				})
			})
		}
	})
}
func (a *App) renameSnippetGroup(from, to string) {
	s := &a.snippets
	s.renaming = ""
	if to == "" || to == from {
		return
	}
	for _, v := range a.Data.Commands {
		if v.Group == from {
			v.Group = to
			if e := a.P.SaveCommand(v); e != nil {
				a.fail(e)
				break
			}
		}
	}
	a.reload()
	if s.group == from {
		s.group = to
	}
}
