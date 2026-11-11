package appui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"nextshell.dev/v2/internal/commandtemplate"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/monitor"
	"nextshell.dev/v2/internal/remoteedit"
	"nextshell.dev/v2/internal/sftpfs"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type phase2State struct {
	MonitorHistories                                                               map[string]*monitor.History
	HistoryDisk, HistoryInterface                                                  string
	Documents                                                                      []*remoteDocument
	ActiveDocument                                                                 int
	CloseDocumentKey                                                               string
	FileSelection                                                                  ui.Selection[string]
	Clipboard                                                                      remoteClipboard
	DiagnosticID, DiagnosticTarget, DiagnosticText                                 string
	DiagnosticBusy                                                                 bool
	TemplateSource, TemplateID                                                     string
	Parameters                                                                     []commandParameter
	TransferFilter, CommandSearch, CommandGroup, MonitorFilter                     string
	ProcessList, NetworkList                                                       ui.ListState
	ProcessRow, NetworkRow                                                         int
	ProcessSort                                                                    ui.SortOrder
	HistoryVisible, ClearHistoryConfirm                                            bool
	FileList                                                                       ui.ListState
	FileRow                                                                        int
	Code                                                                           *editor.State
	CodeSource, CodePath                                                           string
	CodeVersion                                                                    uint64
	ConfirmOverwriteImport                                                         bool
	FilesSession, RemotePath, SelectedRemote, LocalPath, TargetSession, TargetPath string
	Files                                                                          []sftpfs.Entry
	Overwrite, FollowCWD                                                           bool
	Action, ActionFrom, ActionTo                                                   string
	EditorSession                                                                  string
	Document                                                                       remoteedit.Document
	ExternalArgs, EditorText, ExternalProgram                                      string
	OpenSession, OpenPath                                                          string
	MonitorSession, MonitorPage, MonitorWatch                                      string
	Monitor                                                                        monitor.Snapshot
	AutoMonitor                                                                    bool
	PID, Signal                                                                    string
	ConfirmSignal                                                                  bool
	Command                                                                        model.Command
	CommandAll                                                                     bool
	ImportPath, ImportPassword, ImportFormat, ImportPolicy, ImportFolder           string
	Import                                                                         *ImportPreview
	ExportPath, ExportPassword                                                     string
	EncryptExport                                                                  bool
}

func (s *phase2State) init() {
	s.ExternalArgs = "[]"
	s.DiagnosticTarget = "127.0.0.1"
	s.TransferFilter = "全部"
	s.CommandGroup = "全部"
	s.ProcessRow = -1
	s.NetworkRow = -1
	s.ProcessSort = ui.SortOrder{Column: "CPU", Descending: true}
	s.RemotePath = "."
	s.FileRow = -1
	s.Signal = "TERM"
	s.MonitorPage = "system"
	s.ImportFormat = "nextshell"
	s.ImportPolicy = "skip"
	s.EncryptExport = true
	s.Command.Group = "默认"
}
func (a *App) activeID() string {
	if s := a.current(); s != nil {
		return s.ID
	}
	return ""
}
func (a *App) handlePhase2(e Event) {
	s := &a.Phase2
	s.syncEditorText()
	if e.Diagnostic != nil && e.Diagnostic.ID == s.DiagnosticID {
		s.DiagnosticBusy = false
		s.DiagnosticText = e.Diagnostic.Kind + "\n" + e.Diagnostic.Text
	}
	if e.Files != nil && e.Files.Session == a.activeID() {
		if s.FilesSession != e.Files.Session || s.RemotePath != e.Files.Path {
			s.FileSelection.Clear()
			s.FileRow = -1
		}
		s.FilesSession = e.Files.Session
		s.RemotePath = e.Files.Path
		s.Files = e.Files.Entries
	}
	if e.Editor != nil {
		a.receiveDocument(e.Editor)
	}
	if e.Monitor != nil {
		a.recordMonitorHistory(e.Monitor.Session, e.Monitor.Snapshot)
		s.MonitorSession = e.Monitor.Session
		s.Monitor = e.Monitor.Snapshot
	}
	if e.Import != nil {
		s.Import = e.Import
		s.ConfirmOverwriteImport = false
		a.Page = "data"
	}
	if e.Message != "" {
		a.reload()
		if strings.HasPrefix(e.Message, "imported ") {
			s.Import = nil
		}
	}
}
func (a *App) phasePage(c *ui.Context, title string, body func()) {
	ui.Scroll(c).Key("page-" + a.Page).Grow(1).FillWidth().Padding(PageInset).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(1120).Padding(PanelInset).Gap(FormGap).Radius(PanelRadius).Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() { ui.Text(c, title).FontSize(26).Bold(); ui.Form(c, body).FillWidth().Gap(FormGap) })
	})
}
func (a *App) queue(req TransferRequest) {
	id, e := a.P.QueueTransfer(req)
	a.status(e)
	if e == nil {
		a.Message = "已加入传输队列: " + id
	}
}
func (a *App) openEditor(session, remote string) {
	s := &a.Phase2
	s.storeActiveDocument()
	key := documentKey(session, remote)
	for i, d := range s.Documents {
		if d.Key == key {
			s.selectDocument(i)
			a.Page = "editor"
			return
		}
	}
	a.P.OpenRemote(session, remote)
	a.Page = "editor"
}
func (a *App) filesView(c *ui.Context) {
	s := &a.Phase2
	a.phasePage(c, "SFTP 文件", func() {
		id := a.activeID()
		if id == "" {
			ui.Text(c, "请先选择远程 SSH 标签")
			return
		}
		field(c, "远端目录", &s.RemotePath)
		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c, "刷新目录").Clicked() {
				a.P.ListFiles(id, s.RemotePath)
			}
			if ui.Button(c, "上级目录").Clicked() {
				s.RemotePath = path.Dir(s.RemotePath)
				a.P.ListFiles(id, s.RemotePath)
			}
			ui.Checkbox(c, &s.FollowCWD, "跟随终端目录（OSC 7）")
			if ui.Button(c, "跟随当前目录").Clicked() {
				if cwd := a.P.SessionDir(id); cwd != "" {
					s.RemotePath = cwd
					a.P.ListFiles(id, cwd)
				} else {
					a.Message = "终端尚未报告 OSC 7 目录"
				}
			}
		})
		if s.FollowCWD {
			if cwd := a.P.SessionDir(id); cwd != "" && cwd != s.RemotePath {
				s.RemotePath = cwd
				a.P.ListFiles(id, cwd)
			}
		}
		if s.FilesSession != id {
			ui.Text(c, "尚未加载此会话的目录，请刷新")
		} else {
			zone := ui.Column(c).FillWidth().MinHeight(80).Gap(3)
			a.filesBlankMenu(zone, id)
			for _, local := range zone.DroppedFiles() {
				a.queue(TransferRequest{Session: id, Kind: "upload", Local: local, Remote: path.Join(s.RemotePath, filepath.Base(local)), Overwrite: s.Overwrite})
			}
			zone.Children(func() {
				a.fileClipboardToolbar(c, id)
				ui.Text(c, "Ctrl/Shift 多选 · 本地文件可拖入上传（文件夹请用打包上传）")
				s.FileList.Selected = &s.FileRow
				s.FileList.Selection = &s.FileSelection
				s.FileList.RoundRows = true
				s.FileList.Key = func(i int) any { return s.Files[i].Path }
				s.FileList.Label = func(i int) string { return s.Files[i].Name }
				s.FileList.CellMenu = func(m *ui.Menu, row, col int) {
					if row >= 0 && row < len(s.Files) {
						a.fileMenuItems(m, id, s.Files[row])
					}
				}
				cols := []ui.TableColumn{{Title: "名称"}, {Title: "大小", Width: 90, Align: ui.End}, {Title: "权限", Width: 145}, {Title: "操作", Width: 120}}
				table := ui.Table(c.Key("sftp-file-list-"+id), &s.FileList, cols, len(s.Files), func(row, col int) {
					f := s.Files[row]
					switch col {
					case 0:
						label := f.Name
						if f.Symlink {
							label += " [链接]"
						}
						ic := "files"
						if f.Directory {
							ic = "folder"
						}
						actionLabel := "选择 " + f.Name
						if f.Directory {
							actionLabel = "打开 " + f.Name
						}
						ui.Row(c).Label(actionLabel).Gap(8).AlignItems(ui.Center).Children(func() { ui.Icon(c, appIcons[ic]).FontSize(17); ui.Text(c, label).SingleLine() })
					case 1:
						ui.Text(c, fmt.Sprintf("%d B", f.Size)).TextColor(c.Theme().TextMuted).FontSize(12)
					case 2:
						ui.Text(c, f.Mode).TextColor(c.Theme().TextMuted).FontSize(12)
					case 3:
						if !f.Directory && !f.Symlink && ghost(c, "编辑文件", "edit").Clicked() {
							a.openEditor(id, f.Path)
						}
					}
				}).Height(260).FillWidth().Border(1, c.Theme().Border).Radius(c.Theme().Radius)
				a.fileClipboardKeys(table, id)
				if table.Changed() && s.FileRow >= 0 && s.FileRow < len(s.Files) {
					s.SelectedRemote = s.Files[s.FileRow].Path
				}
				if table.Submitted() && s.FileRow >= 0 && s.FileRow < len(s.Files) {
					f := s.Files[s.FileRow]
					if f.Directory {
						s.RemotePath = f.Path
						a.P.ListFiles(id, f.Path)
					} else if !f.Symlink {
						a.openEditor(id, f.Path)
					}
				}

			})
		}
		field(c, "选定/远端文件", &s.SelectedRemote)
		field(c, "本地文件/目录", &s.LocalPath)
		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c, "默认上传目录").Clicked() {
				s.RemotePath = a.Prefs.UploadDir
				a.P.ListFiles(id, s.RemotePath)
			}
			if ui.Button(c, "默认下载位置").Clicked() {
				s.LocalPath = filepath.Join(a.Prefs.DownloadDir, path.Base(s.SelectedRemote))
			}
		})
		ui.Checkbox(c, &s.Overwrite, "允许覆盖现有普通文件（目录打包不覆盖）")
		ui.Row(c).Gap(6).Wrap().Children(func() {
			if ui.Button(c, "上传文件").Clicked() {
				a.queue(TransferRequest{Session: id, Kind: "upload", Local: s.LocalPath, Remote: s.SelectedRemote, Overwrite: s.Overwrite})
			}
			if ui.Button(c, "下载文件").Clicked() {
				a.queue(TransferRequest{Session: id, Kind: "download", Local: s.LocalPath, Remote: s.SelectedRemote, Overwrite: s.Overwrite})
			}
			if ui.Button(c, "打包上传目录").Clicked() {
				a.queue(TransferRequest{Session: id, Kind: "upload-packed", Local: s.LocalPath, Remote: s.SelectedRemote})
			}
			if ui.Button(c, "打包下载目录").Clicked() {
				a.queue(TransferRequest{Session: id, Kind: "download-packed", Local: s.LocalPath, Remote: s.SelectedRemote})
			}
			if ui.Button(c, "打开远程编辑器").Clicked() {
				a.openEditor(id, s.SelectedRemote)
			}
		})
		var ids, names []string
		for _, tab := range a.Tabs {
			if !tab.Local {
				ids = append(ids, tab.ID)
				names = append(names, tab.Title)
			}
		}
		chooseID(c, "跨服务器目标", &s.TargetSession, ids, names)
		field(c, "目标远端文件", &s.TargetPath)
		if ui.Button(c, "跨服务器传送").Clicked() {
			a.queue(TransferRequest{Session: id, TargetSession: s.TargetSession, Kind: "cross", Remote: s.SelectedRemote, Destination: s.TargetPath, Overwrite: s.Overwrite})
		}
		ui.Text(c, "目录/文件操作：删除仅支持文件、链接或空目录")
		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c, "新建远端目录").Clicked() {
				s.Action = "mkdir"
				s.ActionFrom = s.SelectedRemote
			}
			if ui.Button(c, "重命名远端路径").Clicked() {
				s.Action = "rename"
				s.ActionFrom = s.SelectedRemote
				s.ActionTo = s.TargetPath
			}
			if ui.Button(c, "删除远端路径").Clicked() {
				s.Action = "remove"
				s.ActionFrom = s.SelectedRemote
			}
		})
		if s.Action != "" {
			ui.Text(c, s.Action+": "+s.ActionFrom+" → "+s.ActionTo)
			field(c, "操作源路径", &s.ActionFrom)
			if s.Action == "rename" {
				field(c, "操作目标路径", &s.ActionTo)
			}
			ui.Row(c).Gap(6).Children(func() {
				if ui.Button(c, "确认远端操作").Clicked() {
					a.P.FileAction(id, s.Action, s.ActionFrom, s.ActionTo)
					s.Action = ""
				}
				if ui.Button(c, "取消远端操作").Clicked() {
					s.Action = ""
				}
			})
		}
	})
}
func (a *App) transfersView(c *ui.Context) {
	a.phasePage(c, "传输队列", func() {
		ui.Text(c, "并发 2；取消会关闭该任务的独立 SFTP/exec 通道；失败可重试")
		jobs := a.P.Transfers()
		counts := map[string]int{}
		for _, j := range jobs {
			counts[j.State]++
		}
		ui.Text(c, fmt.Sprintf("等待 %d · 运行 %d · 完成 %d · 失败 %d · 已取消 %d", counts["queued"], counts["running"], counts["done"], counts["failed"], counts["cancelled"]))
		ui.Row(c).Gap(8).Children(func() {
			choice(c, "传输状态", &a.Phase2.TransferFilter, []string{"全部", "queued", "running", "done", "failed", "cancelled"})
			if ui.Button(c, "取消全部传输").Clicked() {
				a.P.CancelAllTransfers()
			}
			if ui.Button(c, "清除已完成").Clicked() {
				a.Message = fmt.Sprintf("已清除 %d 个完成任务", a.P.ClearCompletedTransfers())
			}
		})
		if len(jobs) == 0 {
			ui.Text(c, "暂无传输任务，从文件页上传或下载即可开始").TextColor(c.Theme().TextMuted)
		}
		visible := 0
		for _, j := range jobs {
			if a.Phase2.TransferFilter != "全部" && a.Phase2.TransferFilter != j.State {
				continue
			}
			visible++
			ui.Column(c).Key(j.ID).FillWidth().Gap(4).Children(func() {
				ui.Text(c, fmt.Sprintf("%s · %s · %d bytes · 第 %d 次", j.Label, j.State, j.Bytes, j.Attempt))
				elapsed := time.Since(j.Started)
				if !j.Finished.IsZero() {
					elapsed = j.Finished.Sub(j.Started)
				}
				if !j.Started.IsZero() && elapsed > 0 {
					ui.Text(c, fmt.Sprintf("平均 %.1f KiB/s · %.1f 秒（无总量时不虚构百分比/ETA）", float64(j.Bytes)/1024/elapsed.Seconds(), elapsed.Seconds())).TextColor(c.Theme().TextMuted)
				}
				if j.Error != "" {
					ui.Text(c, j.Error)
				}
				ui.Row(c).Gap(5).Children(func() {
					if j.State == "queued" || j.State == "running" {
						if ui.Button(c, "取消传输").Clicked() {
							a.P.CancelTransfer(j.ID)
						}
					} else if j.State == "failed" || j.State == "cancelled" {
						if ui.Button(c, "重试传输").Clicked() {
							a.status(a.P.RetryTransfer(j.ID))
						}
					}
				})
			})
		}
		if visible == 0 && len(jobs) > 0 {
			ui.Text(c, "当前筛选没有任务").TextColor(c.Theme().TextMuted)
		}
	})
}
func (a *App) editorView(c *ui.Context) {
	s := &a.Phase2
	ui.Column(c).Key("editor-page").Grow(1).FillWidth().Padding(PageInset).Gap(FormGap).Children(func() {
		ui.Text(c, "远程文本编辑（UTF-8，最大 8 MiB）").FontSize(26).Bold()
		a.documentTabs(c)
		a.documentCloseDialog(c)
		if s.OpenPath != "" {
			ui.Text(c, "打开另一文件将丢弃当前未保存文本："+s.OpenPath)
			ui.Row(c).Gap(6).Children(func() {
				if ui.Button(c, "确认丢弃并打开").Clicked() {
					s.EditorText = s.Document.Text
					a.P.OpenRemote(s.OpenSession, s.OpenPath)
					s.OpenPath = ""
				}
				if ui.Button(c, "保留当前编辑").Clicked() {
					s.OpenPath = ""
				}
			})
		}
		if s.Document.Path == "" {
			ui.Text(c, "从 SFTP 面板打开一个普通文本文件")
			return
		}
		ui.Text(c, "来源会话: "+s.EditorSession+" · "+s.Document.Path)
		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c, "保存远程文本").Clicked() {
				a.P.SaveRemote(s.EditorSession, s.Document, s.EditorText)
			}
			if ui.Button(c, "重新载入远程文件").Clicked() {
				s.OpenSession = s.EditorSession
				s.OpenPath = s.Document.Path
			}
			if s.EditorText != s.Document.Text {
				ui.Text(c, "● 未保存")
			}
		})
		s.prepareEditorBuffer()
		editor.View(c.Key(documentKey(s.EditorSession, s.Document.Path)), s.Code).Grow(1).FillWidth()
		s.syncEditorText()
		field(c, "外部编辑器程序", &s.ExternalProgram)
		field(c, "编辑器参数 JSON（如 [\"--wait\"]）", &s.ExternalArgs)
		ui.Text(c, "程序必须等待编辑结束；成功后清理明文，失败保留私密恢复文件并提示路径；自动回传检查远端冲突").FontSize(11)
		if ui.Button(c, "用外部编辑器打开").Clicked() {
			if s.EditorText != s.Document.Text {
				a.Message = "请先保存或丢弃内置编辑器的修改"
			} else {
				var args []string
				if err := json.Unmarshal([]byte(s.ExternalArgs), &args); err != nil {
					a.Message = "编辑器参数必须为 JSON 字符串数组"
				} else {
					a.P.ExternalEdit(s.EditorSession, s.Document.Path, s.ExternalProgram, args)
				}
			}
		}
	})
}
func (a *App) monitorView(c *ui.Context) {
	s := &a.Phase2
	a.phasePage(c, "Linux 监控 / 进程 / 网络", func() {
		id := a.activeID()
		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c, "刷新监控").Clicked() {
				a.P.RefreshMonitor(id)
			}
			old := s.AutoMonitor
			ui.Checkbox(c, &s.AutoMonitor, "每 2 秒刷新")
			if old != s.AutoMonitor {
				a.P.WatchMonitor(id, s.AutoMonitor)
				s.MonitorWatch = id
			}
			for _, v := range []struct{ id, label string }{{"system", "系统"}, {"history", "历史曲线"}, {"process", "进程"}, {"network", "网络连接"}} {
				if ui.Button(c, v.label).Clicked() {
					s.MonitorPage = v.id
				}
			}
		})
		if s.AutoMonitor && s.MonitorWatch != id {
			a.P.WatchMonitor(s.MonitorWatch, false)
			a.P.WatchMonitor(id, true)
			s.MonitorWatch = id
		}
		if s.MonitorPage == "history" {
			a.monitorHistoryView(c, id)
			return
		}
		if s.MonitorSession != id || s.Monitor.At.IsZero() {
			ui.Text(c, "请选择远程 SSH 会话并刷新。仅支持 Linux；无数据时不显示假数值。")
			return
		}
		m := s.Monitor
		ui.Text(c, "采样会话: "+s.MonitorSession+" · "+m.At.In(time.FixedZone("CST", 8*3600)).Format("15:04:05 MST"))
		switch s.MonitorPage {
		case "system":
			ui.Text(c, m.Info)
			ui.Text(c, "Load: "+m.Load)
			cpu := "首帧，等待第二次采样"
			if m.HasDelta {
				cpu = fmt.Sprintf("%.1f%%", m.CPUPercent)
			}
			ui.Text(c, "CPU: "+cpu)
			if m.HasDelta {
				ui.Progress(c, clampRatio(m.CPUPercent/100)).FillWidth()
			}
			ui.Text(c, "内存使用率").TextColor(c.Theme().TextMuted)
			ui.Progress(c, memoryRatio(m.MemoryTotal, m.MemoryAvailable)).FillWidth()
			ui.Text(c, fmt.Sprintf("内存 %.1f / %.1f MiB · Swap %.1f / %.1f MiB", float64(m.MemoryTotal-m.MemoryAvailable)/(1<<20), float64(m.MemoryTotal)/(1<<20), float64(m.SwapTotal-m.SwapFree)/(1<<20), float64(m.SwapTotal)/(1<<20)))
			ui.Text(c, fmt.Sprintf("网络（排除 lo） RX %.0f B/s / TX %.0f B/s", m.RXPerSecond, m.TXPerSecond))
			for _, d := range m.Disks {
				ui.Text(c, fmt.Sprintf("%s %s · %.1f%% · %d/%d KiB", d.Device, d.Mount, d.UsedPercent, d.UsedKB, d.TotalKB))
			}
		case "process":
			field(c, "PID", &s.PID)
			choice(c, "信号", &s.Signal, []string{"TERM", "KILL", "STOP", "CONT"})
			ui.Checkbox(c, &s.ConfirmSignal, "确认发送进程信号（可能终止远端程序）")
			if ui.Button(c, "发送进程信号").Clicked() {
				if !s.ConfirmSignal {
					a.Message = "请先确认进程信号"
				} else {
					pid, e := strconv.Atoi(s.PID)
					if e != nil {
						a.status(e)
					} else {
						a.P.SignalProcess(id, pid, s.Signal)
						s.ConfirmSignal = false
					}
				}
			}
			a.processTable(c, m.Processes)
		case "network":
			a.networkTable(c, m.Network)
		}
		for _, w := range m.Warnings {
			ui.Text(c, w)
		}
	})
}
func (a *App) commandsView(c *ui.Context) {
	s := &a.Phase2
	a.phasePage(c, "命令库", func() {
		ui.Text(c, "命令与历史以本地明文保存，请勿输入密码或密钥")
		a.commandHistory(c)
		field(c, "命令名称", &s.Command.Name)
		field(c, "命令分组", &s.Command.Group)
		field(c, "描述", &s.Command.Description)
		appendCR := s.Command.AppendCR == nil || *s.Command.AppendCR
		ui.Checkbox(c, &appendCR, "执行时追加回车")
		s.Command.AppendCR = &appendCR
		ui.TextArea(c, &s.Command.Text).Label("保存的命令").Height(130).FillWidth()
		s.prepareParameters()
		for i := range s.Parameters {
			field(c, "参数 "+s.Parameters[i].Key, &s.Parameters[i].Value)
		}
		if len(s.Parameters) > 0 {
			ui.Text(c, "参数原样替换，不自动shell转义；参数值仅内存，未持久化历史。").TextColor(c.Theme().TextMuted)
			ui.Text(c, "预览："+s.commandText())
		}
		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c, "新命令").Clicked() {
				s.Command = model.Command{Group: "默认"}
			}
			if ui.Button(c, "保存到命令库").Clicked() {
				if s.Command.ID == "" {
					s.Command.ID = a.P.NewID()
				}
				a.status(a.P.SaveCommand(s.Command))
				a.reload()
			}
			if ui.Button(c, "在当前会话执行").Clicked() {
				if e := a.P.ExecuteCommand(a.activeID(), s.commandText(), false, appendCR); e != nil {
					a.status(e)
				} else {
					a.Page = "terminal"
					a.Message = "命令已发送到当前会话"
				}
			}
		})
		ui.Checkbox(c, &s.CommandAll, "确认将当前命令发往全部打开会话")
		if ui.Button(c, "在全部会话执行").Clicked() {
			if !s.CommandAll {
				a.Message = "请先确认全部会话执行"
			} else {
				a.status(a.P.ExecuteCommand(a.activeID(), s.commandText(), true, appendCR))
				s.CommandAll = false
			}
		}
		ui.SearchField(c, &s.CommandSearch).Label("搜索命令库").Placeholder("名称 / 分组 / 描述 / 命令内容").FillWidth()
		groups := []string{"全部"}
		for _, v := range a.Data.Commands {
			if !contains(groups, v.Group) {
				groups = append(groups, v.Group)
			}
		}
		choice(c, "筛选命令分组", &s.CommandGroup, groups)
		visible := 0
		for _, command := range a.Data.Commands {
			if !commandMatches(command, s.CommandSearch, s.CommandGroup) {
				continue
			}
			visible++
			row := ui.Row(c).Key(command.ID).Label("命令行 " + command.Name).Gap(6)
			a.commandMenu(row, command)
			row.Children(func() {
				if ui.Button(c, command.Group+" / "+command.Name).Grow(1).Clicked() {
					s.Command = command
				}
				if ui.Button(c, "删除保存命令").Clicked() {
					a.DeleteKind = "command"
					a.DeleteID = command.ID
				}
			})
		}
		if visible == 0 {
			ui.Text(c, "没有匹配的命令；可新建命令或清除筛选").TextColor(c.Theme().TextMuted)
		}
	})
}
func (a *App) dataView(c *ui.Context) {
	s := &a.Phase2
	a.phasePage(c, "导入 / 导出（v1 连接文件）", func() {
		ui.Text(c, "导入先预览再确认，密码不会在预览显示；XOR 旧导出按计划不支持")
		field(c, "导入文件/目录", &s.ImportPath)
		choice(c, "来源格式", &s.ImportFormat, []string{"nextshell", "finalshell"})
		password(c, "解密口令", &s.ImportPassword)
		a.folderChoice(c, "目标目录", &s.ImportFolder)
		choice(c, "已有主机策略", &s.ImportPolicy, []string{"skip", "rename-copy", "overwrite"})
		if ui.Button(c, "预览导入").Clicked() {
			a.P.PreviewImport(s.ImportPath, s.ImportPassword, s.ImportFormat)
			s.ImportPassword = ""
		}
		if s.Import != nil {
			ui.Text(c, fmt.Sprintf("预览 %d 条 · 密码不可恢复 %d · 需重绑密钥 %d", s.Import.Count, s.Import.PasswordsUnavailable, s.Import.KeysNeedRebind))
			ui.Text(c, strings.Join(s.Import.Names, ", "))
			for _, w := range s.Import.Warnings {
				ui.Text(c, w)
			}
			for _, target := range s.Import.Targets {
				ui.Text(c, "现有目标："+target)
			}
			if s.ImportPolicy == "overwrite" {
				ui.Checkbox(c, &s.ConfirmOverwriteImport, "确认覆盖列出的唯一目标；文件明确携带的空密码将清空密码")
			}
			if ui.Button(c, "确认导入预览连接").Clicked() {
				if s.ImportPolicy == "overwrite" && !s.ConfirmOverwriteImport {
					a.Message = "请先确认覆盖"
					return
				}
				a.P.CommitImport(s.Import.Token, s.ImportFolder, s.ImportPolicy)
			}
		}
		ui.Text(c, "导出：连接管理中勾选的连接；未勾选则导出全部非回收连接").Bold()
		field(c, "新导出文件路径", &s.ExportPath)
		ui.Checkbox(c, &s.EncryptExport, "加密导出（含密码，b64## + v1 格式）")
		password(c, "导出口令", &s.ExportPassword)
		if !s.EncryptExport {
			ui.Text(c, "明文导出会省略所有密码，永不导出私钥")
		}
		if ui.Button(c, "写入新导出文件").Clicked() {
			var pass *string
			if s.EncryptExport {
				if s.ExportPassword == "" {
					a.Message = "加密导出需输入非空口令"
					return
				}
				copy := s.ExportPassword
				pass = &copy
			}
			var ids []string
			for id, chosen := range a.Selected {
				if chosen {
					ids = append(ids, id)
				}
			}
			a.P.ExportConnections(s.ExportPath, pass, ids)
			s.ExportPassword = ""
		}
	})
}

// WriteTo (not String) preserves the official buffer's CRLF save representation.
func (s *phase2State) syncEditorText() {
	if s.Code == nil || s.Code.Buffer == nil || s.Code.Buffer.Version() == s.CodeVersion {
		return
	}
	var out bytes.Buffer
	s.Code.Buffer.WriteTo(&out)
	s.EditorText = out.String()
	s.CodeSource = s.EditorText
	s.CodeVersion = s.Code.Buffer.Version()
}
func (s *phase2State) prepareEditorBuffer() {
	key := documentKey(s.EditorSession, s.Document.Path)
	if s.Code == nil || s.Code.Buffer == nil || s.CodePath != key || s.CodeSource != s.EditorText {
		s.Code = &editor.State{Buffer: editor.NewBuffer(s.EditorText), Label: "远程文本"}
		s.Code.Buffer.SetLanguage(editor.LanguageFor(s.Document.Path))
		s.CodePath = key
		s.CodeSource = s.EditorText
		s.CodeVersion = s.Code.Buffer.Version()
	}
}

type commandParameter struct{ Key, Value string }

func (s *phase2State) prepareParameters() {
	if s.TemplateSource == s.Command.Text && s.TemplateID == s.Command.ID {
		return
	}
	old := map[string]string{}
	if s.TemplateID == s.Command.ID {
		for _, v := range s.Parameters {
			old[v.Key] = v.Value
		}
	}
	s.Parameters = nil
	for _, key := range commandtemplate.Keys(s.Command.Text) {
		s.Parameters = append(s.Parameters, commandParameter{key, old[key]})
	}
	s.TemplateID = s.Command.ID
	s.TemplateSource = s.Command.Text
}
func (s *phase2State) commandText() string {
	params := map[string]string{}
	for _, v := range s.Parameters {
		params[v.Key] = v.Value
	}
	return commandtemplate.Render(s.Command.Text, params)
}
