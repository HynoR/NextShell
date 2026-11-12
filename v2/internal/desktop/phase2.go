package desktop

import (
	"context"
	"errors"
	"fmt"
	"github.com/pkg/sftp"
	"io"
	"io/fs"
	"nextshell.dev/v2/internal/appui"
	"nextshell.dev/v2/internal/exchange"
	"nextshell.dev/v2/internal/model"
	"nextshell.dev/v2/internal/monitor"
	"nextshell.dev/v2/internal/remoteedit"
	"nextshell.dev/v2/internal/sftpfs"
	"nextshell.dev/v2/internal/sshclient"
	"nextshell.dev/v2/internal/storage"
	"nextshell.dev/v2/internal/transfer"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func (p *Platform) ssh(id string) (*sshclient.Session, error) {
	p.mu.Lock()
	s := p.sshSessions[id]
	p.mu.Unlock()
	if s == nil {
		return nil, errors.New("请先切换到一个远程 SSH 会话")
	}
	return s, nil
}
func (p *Platform) withSFTP(ctx context.Context, id string, fn func(*sftpfs.Client) error) error {
	s, e := p.ssh(id)
	if e != nil {
		return e
	}
	c, cleanup, e := s.OpenSFTPContext(ctx)
	if e != nil {
		return e
	}
	defer cleanup()
	fs := &sftpfs.Client{Client: c}
	if e = ctx.Err(); e != nil {
		return e
	}
	return fn(fs)
}
func (p *Platform) operation(fn func(context.Context) error) { p.operationFor(false, fn) }

// operationFor is operation for a feature of the editor window when forEditor
// is set: its failures are tagged so the UI shows them there.
func (p *Platform) operationFor(forEditor bool, fn func(context.Context) error) {
	p.launch(func() {
		ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
		defer cancel()
		if e := fn(ctx); e != nil {
			p.emit(appui.Event{Message: e.Error(), ForEditor: forEditor})
		}
	})
}
func (p *Platform) ListFiles(session, remote string) {
	p.operation(func(ctx context.Context) error {
		return p.withSFTP(ctx, session, func(c *sftpfs.Client) error {
			if remote == "" {
				remote = "."
			}
			if remote == "~" || strings.HasPrefix(remote, "~/") {
				home, err := c.RealPath(".")
				if err != nil {
					return err
				}
				remote = path.Join(home, strings.TrimPrefix(strings.TrimPrefix(remote, "~"), "/"))
			}
			resolved, e := c.RealPath(remote)
			if e != nil {
				return e
			}
			entries, e := c.List(resolved)
			if e != nil {
				return e
			}
			p.emit(appui.Event{Files: &appui.FileListing{Session: session, Path: resolved, Entries: entries}})
			return nil
		})
	})
}
func (p *Platform) FileAction(session, action, from, to string) {
	p.operation(func(ctx context.Context) error {
		return p.withSFTP(ctx, session, func(c *sftpfs.Client) error {
			if e := sftpfs.CheckPath(from); e != nil {
				return e
			}
			var e error
			switch action {
			case "create":
				var f *sftp.File
				f, e = c.OpenFile(from, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
				if e == nil {
					e = f.Close()
				}
			case "mkdir":
				e = c.Mkdir(from)
			case "rename":
				e = c.RenamePath(from, to)
			case "remove":
				e = c.RemovePath(from)
			default:
				return errors.New("unsupported file action")
			}
			if e == nil {
				p.emit(appui.Event{Message: "文件操作完成"})
				p.ListFiles(session, path.Dir(from))
			}
			return e
		})
	})
}
func (p *Platform) QueueTransfer(req appui.TransferRequest) (string, error) {
	local, err := LocalPath(req.Local)
	if err != nil {
		return "", err
	}
	req.Local = local
	if _, e := p.ssh(req.Session); e != nil {
		return "", e
	}
	switch req.Kind {
	case "upload", "download", "upload-packed", "download-packed", "cross", "copy", "move":
	default:
		return "", errors.New("unsupported transfer kind")
	}
	if req.Kind == "cross" || req.Kind == "copy" || req.Kind == "move" {
		if _, e := p.ssh(req.TargetSession); e != nil {
			return "", e
		}
	}
	sameServer := req.Session == req.TargetSession
	if req.Kind == "cross" || req.Kind == "copy" || req.Kind == "move" {
		p.mu.Lock()
		src, srcOK := p.credentials[req.Session]
		dst, dstOK := p.credentials[req.TargetSession]
		p.mu.Unlock()
		sameServer = sameServer || (srcOK && dstOK && src.Address() == dst.Address() && src.User == dst.User && src.ProxyID == dst.ProxyID && src.Fingerprint != "" && src.Fingerprint == dst.Fingerprint)
		if req.Kind == "move" && !sameServer {
			return "", errors.New("cut/paste is only available on the same server/account")
		}
	}
	id, e := p.queue.Add(transferLabel(req), func(ctx context.Context, progress func(int64)) error {
		if req.Kind == "upload-packed" || req.Kind == "download-packed" {
			s, e := p.ssh(req.Session)
			if e != nil {
				return e
			}
			if req.Kind == "upload-packed" {
				return transfer.UploadPacked(ctx, s, req.Local, req.Remote, progress)
			}
			return transfer.DownloadPacked(ctx, s, req.Remote, req.Local, progress)
		}
		return p.withSFTP(ctx, req.Session, func(c *sftpfs.Client) error {
			switch req.Kind {
			case "upload":
				info, e := os.Stat(req.Local)
				if e != nil {
					return e
				}
				if info.IsDir() {
					// Directories go as one tar stream when the server has tar.
					if s, e := p.ssh(req.Session); e == nil && p.canPack(ctx, req.Session, s) {
						return transfer.UploadPacked(ctx, s, req.Local, req.Remote, progress)
					}
					transfer.SetTotal(ctx, localSize(req.Local))
					return c.UploadTree(ctx, req.Local, req.Remote, progress)
				}
				transfer.SetTotal(ctx, info.Size())
				return c.Upload(ctx, req.Local, req.Remote, req.Overwrite, progress)
			case "download":
				info, e := c.Stat(req.Remote)
				if e != nil {
					return e
				}
				if info.IsDir() {
					if s, e := p.ssh(req.Session); e == nil && p.canPack(ctx, req.Session, s) {
						return transfer.DownloadPacked(ctx, s, req.Remote, req.Local, progress)
					}
					return c.DownloadTree(ctx, req.Remote, req.Local, progress)
				}
				if e = os.MkdirAll(filepath.Dir(req.Local), 0o755); e != nil { // a default folder that does not exist yet
					return e
				}
				transfer.SetTotal(ctx, info.Size())
				return c.Download(ctx, req.Remote, req.Local, req.Overwrite, progress)
			case "move":
				return c.RenamePath(req.Remote, req.Destination)
			case "cross", "copy":
				if info, e := c.Lstat(req.Remote); e == nil && info.Mode().IsRegular() {
					transfer.SetTotal(ctx, info.Size())
				}
				return p.withSFTP(ctx, req.TargetSession, func(target *sftpfs.Client) error {
					if req.Kind == "cross" && req.Overwrite {
						info, err := c.Lstat(req.Remote)
						if err != nil {
							return err
						}
						if info.Mode().IsRegular() {
							return sftpfs.Cross(ctx, c, target, req.Remote, req.Destination, true, progress)
						}
					}
					return sftpfs.CopyTree(ctx, c, target, req.Remote, req.Destination, sameServer, progress)
				})
			}
			return errors.New("invalid transfer")
		})
	})
	return id, e
}

// canPack reports whether the server has tar and a GNU-style `mv -T -n`
// (packed uploads rely on both); a failed probe means recursive SFTP.
func (p *Platform) canPack(ctx context.Context, id string, s *sshclient.Session) bool {
	p.mu.Lock()
	v, ok := p.tar[id]
	p.mu.Unlock()
	if ok {
		return v
	}
	v = s.Run(ctx, packProbe, nil, io.Discard, io.Discard) == nil
	p.mu.Lock()
	p.tar[id] = v
	p.mu.Unlock()
	return v
}
func localSize(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, e := d.Info(); e == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}
func transferLabel(r appui.TransferRequest) string {
	switch r.Kind {
	case "upload", "upload-packed":
		return "上传 " + filepath.Base(r.Local) + " → " + path.Dir(r.Remote)
	case "download", "download-packed":
		return "下载 " + path.Base(r.Remote) + " → " + filepath.Dir(r.Local)
	case "move":
		return "移动 " + path.Base(r.Remote) + " → " + path.Dir(r.Destination)
	default:
		return "复制 " + path.Base(r.Remote) + " → " + path.Dir(r.Destination)
	}
}
func (p *Platform) Transfers() []transfer.Job           { return p.queue.List() }
func (p *Platform) CancelTransfer(id string)            { p.queue.Cancel(id) }
func (p *Platform) RetryTransfer(id string) error       { return p.queue.Retry(id) }
func (p *Platform) OpenRemote(session, remote string)   { p.openRemote(session, remote, false) }
func (p *Platform) ReloadRemote(session, remote string) { p.openRemote(session, remote, true) }
func (p *Platform) openRemote(session, remote string, forEditor bool) {
	p.operationFor(forEditor, func(ctx context.Context) error {
		return p.withSFTP(ctx, session, func(c *sftpfs.Client) error {
			doc, e := remoteedit.Open(c, remote)
			if e != nil {
				return e
			}
			p.emit(appui.Event{Editor: &appui.EditorResult{Session: session, Document: doc}})
			return nil
		})
	})
}
func (p *Platform) SaveRemote(session string, doc remoteedit.Document, text string) {
	p.operationFor(true, func(ctx context.Context) error {
		return p.withSFTP(ctx, session, func(c *sftpfs.Client) error {
			next, e := remoteedit.Save(ctx, c, doc, text)
			if e != nil {
				return e
			}
			p.emit(appui.Event{Editor: &appui.EditorResult{Session: session, Document: next, Saved: true}, Message: "已保存远程文件", ForEditor: true})
			return nil
		})
	})
}
func (p *Platform) ExternalEdit(session, remote, executable string, args []string) {
	args = append([]string{}, args...)
	if strings.HasSuffix(executable, ".app") { // a macOS bundle chosen in the settings' panel
		args = append([]string{"-W", "-a", executable}, args...)
		executable = "/usr/bin/open"
	}
	p.launch(func() {
		e := p.withSFTP(p.ctx, session, func(c *sftpfs.Client) error {
			doc, e := remoteedit.Open(c, remote)
			if e != nil {
				return e
			}
			return remoteedit.External(p.ctx, c, doc, watchEditor, executable, func(next remoteedit.Document) {
				p.emit(appui.Event{Editor: &appui.EditorResult{Session: session, Document: next, Saved: true}, Message: "外部编辑器的修改已保存到远程", ForEditor: true})
			}, args...)
		})
		if e != nil {
			p.emit(appui.Event{Message: e.Error(), ForEditor: true})
		} else {
			p.emit(appui.Event{Message: "外部编辑器已关闭", ForEditor: true})
		}
	})
}
func (p *Platform) RefreshMonitor(session string) {
	p.mu.Lock()
	if p.monitorBusy[session] || p.closed {
		p.mu.Unlock()
		return
	}
	p.monitorBusy[session] = true
	previous, have := p.monitorPrevious[session]
	detail := p.monitorDetail[session]
	p.mu.Unlock()
	p.launch(func() {
		light := false // this probe was light: if detail was switched on meanwhile, its refresh was refused as busy
		defer func() {
			p.mu.Lock()
			delete(p.monitorBusy, session)
			again := light && p.monitorDetail[session]
			p.mu.Unlock()
			if again {
				p.RefreshMonitor(session)
			}
		}()
		s, e := p.ssh(session)
		if e != nil {
			p.emit(appui.Event{Message: e.Error()})
			return
		}
		var old *monitor.Snapshot
		if have {
			old = &previous
		}
		next, e := monitor.Collect(p.ctx, s, old, detail)
		if e != nil {
			p.emit(appui.Event{Message: e.Error()})
			return
		}
		p.mu.Lock()
		p.monitorPrevious[session] = next
		light = !next.Detail
		p.mu.Unlock()
		p.emit(appui.Event{Monitor: &appui.MonitorResult{Session: session, Snapshot: next}})
	})
}

// WatchMonitorDetail makes this session's samples use the full probe (processes and connections) while on,
// the light one otherwise. Turning it on refreshes at once; the 2 s ticker of WatchMonitor reads the flag per tick.
func (p *Platform) WatchMonitorDetail(session string, on bool) {
	p.mu.Lock()
	was := p.monitorDetail[session]
	if on {
		p.monitorDetail[session] = true
	} else {
		delete(p.monitorDetail, session)
	}
	p.mu.Unlock()
	if on && !was {
		p.RefreshMonitor(session)
	}
}
func (p *Platform) WatchMonitor(session string, on bool) {
	p.mu.Lock()
	if cancel := p.monitorWatches[session]; cancel != nil {
		cancel()
		delete(p.monitorWatches, session)
	}
	if !on || p.closed {
		p.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(p.ctx)
	p.monitorWatches[session] = cancel
	p.mu.Unlock()
	p.launch(func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		p.RefreshMonitor(session)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.RefreshMonitor(session)
			}
		}
	})
}
func (p *Platform) SignalProcess(session string, pid int, signal string) {
	p.operation(func(ctx context.Context) error {
		cmd, e := monitor.SignalCommand(pid, signal)
		if e != nil {
			return e
		}
		s, e := p.ssh(session)
		if e != nil {
			return e
		}
		_, e = s.Output(ctx, cmd, 4096)
		if e == nil {
			p.emit(appui.Event{Message: "已发送进程信号"})
			p.RefreshMonitor(session)
		}
		return e
	})
}
func (p *Platform) SaveCommand(c model.Command) error { return p.store.SaveCommand(c) }
func (p *Platform) ExecuteCommand(session, text string, all bool, appendCR ...bool) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("命令为空")
	}
	suffix := "\r"
	if len(appendCR) > 0 && !appendCR[0] {
		suffix = ""
	}
	if !all {
		return p.Send(session, text+suffix)
	}
	p.mu.Lock()
	ids := make([]string, 0, len(p.sessions))
	for id := range p.sessions {
		ids = append(ids, id)
	}
	p.mu.Unlock()
	if len(ids) == 0 {
		return errors.New("没有打开的会话")
	}
	var errs []string
	for _, id := range ids {
		if e := p.Send(id, text+suffix); e != nil {
			errs = append(errs, id+": "+e.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
func readImportFile(file string) ([]byte, error) {
	info, e := os.Lstat(file)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("import source must be a regular file")
	}
	f, e := os.Open(file)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 64<<20+1))
	if len(b) > 64<<20 {
		return nil, errors.New("import file exceeds 64 MiB")
	}
	return b, e
}
func (p *Platform) PreviewImport(file, password, format string) {
	p.operation(func(ctx context.Context) error {
		if format != "nextshell" && format != "finalshell" {
			return errors.New("unsupported import format")
		}
		info, e := os.Lstat(file)
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("import source cannot be a symlink")
		}
		var entries []exchange.Entry
		plainCredentials := false
		parse := func(file, override string) error {
			raw, e := readImportFile(file)
			if e != nil {
				return e
			}
			var parsed []exchange.Entry
			if format == "finalshell" {
				parsed, e = exchange.DecodeFinalShell(raw, override)
			} else {
				pass := &password
				f, err := exchange.Decode(raw, pass)
				e = err
				parsed = f.Connections
				if f.PlainCredentials {
					plainCredentials = true
				}
				if override != "" {
					for i := range parsed {
						parsed[i].GroupPath = override
					}
				}
			}
			if e != nil {
				return e
			}
			if len(entries)+len(parsed) > 100000 {
				return errors.New("import exceeds 100000 total connections")
			}
			entries = append(entries, parsed...)
			return nil
		}
		var warnings []string
		if info.IsDir() {
			files := 0
			total := int64(0)
			e = filepath.WalkDir(file, func(name string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if e = ctx.Err(); e != nil {
					return e
				}
				if d.Type()&os.ModeSymlink != 0 {
					warnings = append(warnings, "skipped symlink "+d.Name())
					return nil
				}
				if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
					return nil
				}
				files++
				if files > 5000 {
					return errors.New("import directory exceeds 5000 files")
				}
				stat, e := d.Info()
				if e != nil {
					return e
				}
				total += stat.Size()
				if total > 64<<20 {
					return errors.New("import directory exceeds 64 MiB")
				}
				rel, _ := filepath.Rel(file, filepath.Dir(name))
				override := "/import"
				if rel != "." {
					override += "/" + filepath.ToSlash(rel)
				}
				if e = parse(name, override); e != nil {
					warnings = append(warnings, filepath.Base(name)+": "+e.Error())
				}
				return nil
			})
			if e != nil {
				return e
			}
		} else if e = parse(file, ""); e != nil {
			return e
		}
		if len(entries) == 0 {
			return errors.New("no importable connections")
		}
		if plainCredentials {
			warnings = append(warnings, "此文件含明文密码；确认后将使用 v2 设备密钥重新加密保存")
		}
		generation, e := p.store.Generation()
		if e != nil {
			return e
		}
		keys, e := p.store.Keys()
		if e != nil {
			return e
		}
		localKeys := []model.Key{}
		for _, k := range keys {
			if k.ScopeID() == model.LocalScope {
				localKeys = append(localKeys, k)
			}
		}
		keys = localKeys
		existing, e := p.store.Connections()
		if e != nil {
			return e
		}
		preview := &appui.ImportPreview{Token: p.NewID(), Count: len(entries), Warnings: warnings}
		for _, entry := range entries {
			candidate := exchange.FromEntry(entry, keys)
			for _, old := range existing {
				if old.ScopeID() == model.LocalScope && !old.Deleted && endpoint(old) == endpoint(candidate) {
					preview.Targets = append(preview.Targets, old.ID+" · "+old.Name)
				}
			}
			if len(preview.Names) < 100 {
				preview.Names = append(preview.Names, entry.Name)
			}
			if entry.PasswordUnavailable {
				preview.PasswordsUnavailable++
			}
			if c := exchange.FromEntry(entry, keys); c.NeedsKeyRebind {
				preview.KeysNeedRebind++
			}
		}
		p.mu.Lock()
		if len(p.imports) >= 20 {
			p.mu.Unlock()
			return errors.New("too many pending import previews; restart to clear")
		}
		after, e := p.store.Generation()
		if e != nil {
			p.mu.Unlock()
			return e
		}
		if after != generation {
			p.mu.Unlock()
			return errors.New("profile changed during preview; retry")
		}
		p.imports[preview.Token] = importPlan{Entries: entries, Generation: generation}
		p.mu.Unlock()
		p.emit(appui.Event{Import: preview})
		return nil
	})
}

type importPlan struct {
	Entries    []exchange.Entry
	Generation int64
}

func endpoint(c model.Connection) string { return c.User + "\x00" + c.Address() }
func (p *Platform) CommitImport(token, folder, policy string) {
	p.operation(func(ctx context.Context) error {
		if policy == "" {
			policy = "skip"
		}
		if policy != "skip" && policy != "rename-copy" && policy != "overwrite" {
			return errors.New("import policy must be skip, rename-copy or overwrite")
		}
		p.mu.Lock()
		plan, ok := p.imports[token]
		p.mu.Unlock()
		if !ok {
			return errors.New("import preview expired")
		}
		g, e := p.store.Generation()
		if e != nil {
			return e
		}
		if g != plan.Generation {
			return errors.New("import preview is stale; preview again")
		}
		snapshot, e := p.Snapshot()
		if e != nil {
			return e
		}
		folders := map[string]model.Folder{}
		byName := map[string]string{}
		for _, f := range snapshot.Folders {
			if f.ScopeID() != model.LocalScope {
				continue
			}
			folders[f.ID] = f
			byName[f.ParentID+"\x00"+f.Name] = f.ID
		}
		if folder != "" {
			if _, ok := folders[folder]; !ok {
				return errors.New("target import folder not found in local scope")
			}
		}
		keys := []model.Key{}
		for _, k := range snapshot.Keys {
			if k.ScopeID() == model.LocalScope {
				keys = append(keys, k)
			}
		}
		b := storage.Bundle{Overwrite: map[string]bool{}, ExpectedGeneration: &plan.Generation}
		conflicts := map[string][]model.Connection{}
		names := map[string]bool{}
		for _, c := range snapshot.Connections {
			if c.ScopeID() == model.LocalScope && !c.Deleted {
				conflicts[endpoint(c)] = append(conflicts[endpoint(c)], c)
				names[c.Name] = true
			}
		}
		usedTargets := map[string]bool{}
		skipped := 0
		credentials := map[string]model.Credential{} // local credentials by name
		for _, cr := range snapshot.Credentials {
			if cr.ScopeID() == model.LocalScope {
				credentials[cr.Name] = cr
			}
		}
		imported := map[exchange.CredentialRef]string{} // a credential the file repeats per host is created once
		for _, entry := range plan.Entries {
			c := exchange.FromEntry(entry, keys)
			matches := conflicts[endpoint(c)]
			if policy == "skip" && len(matches) > 0 {
				skipped++
				continue
			}
			c.ID = p.NewID()
			overwriting := policy == "overwrite" && len(matches) > 0
			if overwriting {
				if len(matches) != 1 || usedTargets[matches[0].ID] {
					return errors.New("ambiguous overwrite endpoint; choose rename-copy or skip")
				}
				old, e := p.store.Connection(matches[0].ID)
				if e != nil {
					return e
				}
				c = exchange.MergeEntry(old, entry, keys)
				b.Overwrite[c.ID] = true
				usedTargets[c.ID] = true
			} else {
				if policy == "rename-copy" {
					base := c.Name
					for n := 2; names[c.Name]; n++ {
						c.Name = fmt.Sprintf("%s（导入 %d）", base, n)
					}
				}
				names[c.Name] = true
			}
			if !overwriting || entry.Has("groupPath") {
				parent := folder
				for _, name := range exchange.Segments(entry.GroupPath) {
					k := parent + "\x00" + name
					id := byName[k]
					if id == "" {
						id = p.NewID()
						byName[k] = id
						b.Folders = append(b.Folders, model.Folder{ID: id, Name: name, ParentID: parent})
					}
					parent = id
				}
				c.FolderID = parent
			}
			if r := entry.Credential; r != nil {
				same := *r
				same.Password, same.KeyRef = nil, nil
				if id := imported[same]; id != "" {
					c.CredentialID = id
				} else if cr, ok := exchange.FromCredential(*r, keys); ok {
					old, found := credentials[cr.Name]
					if found && old.User == cr.User && old.Auth == cr.Auth {
						c.CredentialID = old.ID
					} else {
						for n := 2; found; n++ {
							cr.Name = fmt.Sprintf("%s（导入 %d）", r.Name, n)
							_, found = credentials[cr.Name]
						}
						cr.ID = p.NewID()
						credentials[cr.Name] = cr
						b.Credentials = append(b.Credentials, cr)
						c.CredentialID = cr.ID
					}
					imported[same] = c.CredentialID
				}
			}
			b.Connections = append(b.Connections, c)
			if !overwriting {
				conflicts[endpoint(c)] = append(conflicts[endpoint(c)], c)
			}
		}
		if e = p.store.ApplyBundle(b); e != nil {
			return e
		}
		p.mu.Lock()
		delete(p.imports, token)
		p.mu.Unlock()
		p.emit(appui.Event{Message: fmt.Sprintf("已导入 %d 个连接，跳过 %d 个", len(b.Connections), skipped)})
		return nil
	})
}
func (p *Platform) ExportConnections(file string, password *string, ids []string) {
	ids = append([]string{}, ids...)
	var pass *string
	if password != nil {
		copy := *password
		pass = &copy
	}
	p.operation(func(ctx context.Context) error {
		snap, e := p.Snapshot()
		if e != nil {
			return e
		}
		selected := map[string]bool{}
		for _, id := range ids {
			selected[id] = true
		}
		folderMap := map[string]model.Folder{}
		for _, f := range snap.Folders {
			folderMap[f.ID] = f
		}
		keyMap := map[string]model.Key{}
		for _, k := range snap.Keys {
			keyMap[k.ID] = k
		}
		var entries []exchange.Entry
		for _, meta := range snap.Connections {
			if meta.ScopeID() != model.LocalScope || meta.Deleted || (len(ids) > 0 && !selected[meta.ID]) {
				continue
			}
			if strings.EqualFold(meta.Encoding, "shift_jis") {
				return errors.New("shift_jis cannot be represented in v1 export; choose a compatible encoding or deselect it")
			}
			c, e := p.store.Connection(meta.ID)
			if e != nil {
				return e
			}
			var segments []string
			for id := c.FolderID; id != ""; {
				f, ok := folderMap[id]
				if !ok {
					return errors.New("missing folder during export")
				}
				segments = append([]string{f.Name}, segments...)
				id = f.ParentID
			}
			group := "/server"
			if len(segments) > 0 {
				group += "/" + strings.Join(segments, "/")
			}
			var ref *exchange.CredentialRef
			if c.CredentialID != "" { // the host carries the credential's user and auth, and the credential itself
				cr, e := p.store.Credential(c.CredentialID)
				if e != nil {
					return e
				}
				var ck *model.Key
				if v, ok := keyMap[cr.KeyID]; ok {
					ck = &v
				}
				ref = exchange.ToCredential(cr, ck)
				c = c.WithCredential(cr)
			}
			var k *model.Key
			if v, ok := keyMap[c.KeyID]; ok {
				k = &v
			}
			entry := exchange.ToEntry(c, group, k)
			entry.Credential = ref
			entries = append(entries, entry)
		}
		raw, e := exchange.Encode(entries, pass, time.Now())
		if e != nil {
			return e
		}
		f, e := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		_, e = f.Write(raw)
		closeErr := f.Close()
		if e != nil {
			os.Remove(file)
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		p.emit(appui.Event{Message: fmt.Sprintf("已导出 %d 个连接", len(entries))})
		return nil
	})
}

func (p *Platform) SessionDir(id string) string {
	p.mu.Lock()
	t := p.sessions[id]
	p.mu.Unlock()
	if t == nil {
		return ""
	}
	return t.Dir()
}

func (p *Platform) CancelAllTransfers()          { p.queue.CancelAll() }
func (p *Platform) ClearCompletedTransfers() int { return p.queue.ClearCompleted() }

const packProbe = `command -v tar >/dev/null 2>&1 && d=$(mktemp -d 2>/dev/null) && { mv -T -n -- "$d" "$d.t" 2>/dev/null; r=$?; rm -rf -- "$d" "$d.t"; exit $r; }`
