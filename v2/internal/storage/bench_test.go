package storage

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"nextshell.dev/v2/internal/model"
)

// A heavy-user profile: 50 folders (some nested) and 500 hosts with tags, notes and a stored password.
const (
	benchFolders = 50
	benchHosts   = 500
)

func benchFolderList() []model.Folder {
	fs := make([]model.Folder, benchFolders)
	for i := range fs {
		fs[i] = model.Folder{ID: fmt.Sprintf("folder-%02d", i), Name: fmt.Sprintf("环境 %02d", i), SortIndex: i}
		if i >= 10 {
			fs[i].ParentID = fmt.Sprintf("folder-%02d", i%10)
		}
	}
	return fs
}

func benchHostList() []model.Connection {
	cs := make([]model.Connection, benchHosts)
	for i := range cs {
		cs[i] = model.Connection{
			ID: fmt.Sprintf("host-%03d", i), Name: fmt.Sprintf("prod-web-%03d 生产", i), Host: fmt.Sprintf("10.%d.%d.%d", i/250, i/25%10, i%250+1), Port: 22, User: "deploy", Auth: "password",
			FolderID: fmt.Sprintf("folder-%02d", i%benchFolders), Tags: []string{"prod", fmt.Sprintf("rack-%d", i%7)}, Notes: "nightly backup target; owner: ops", Encoding: "utf-8", Keepalive: 30,
		}
	}
	return cs
}

func benchStore(b *testing.B) *Store {
	b.Helper()
	s, err := Open(filepath.Join(b.TempDir(), "profile.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { s.Close() })
	for _, f := range benchFolderList() {
		if err = s.SaveFolder(f); err != nil {
			b.Fatal(err)
		}
	}
	secret := "p@ssw0rd-0123456789"
	for _, c := range benchHostList() {
		if err = s.SaveConnection(c, &secret); err != nil {
			b.Fatal(err)
		}
	}
	return s
}

// BenchmarkStoreImport500Hosts: first-run import / cloud restore: 50 folders + 500 hosts into an empty profile.
func BenchmarkStoreImport500Hosts(b *testing.B) {
	folders, hosts := benchFolderList(), benchHostList()
	secret := "p@ssw0rd-0123456789"
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		s, err := Open(filepath.Join(b.TempDir(), "profile.db"))
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		for _, f := range folders {
			if err = s.SaveFolder(f); err != nil {
				b.Fatal(err)
			}
		}
		for _, c := range hosts {
			if err = s.SaveConnection(c, &secret); err != nil {
				b.Fatal(err)
			}
		}
		b.StopTimer()
		s.Close()
	}
}

// BenchmarkStoreSaveConnection: edit-and-save of one host in a populated profile.
func BenchmarkStoreSaveConnection(b *testing.B) {
	s := benchStore(b)
	hosts := benchHostList()
	secret := "p@ssw0rd-0123456789"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := hosts[i%benchHosts]
		c.Notes = fmt.Sprintf("edit %d", i)
		if err := s.SaveConnection(c, &secret); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStoreList500: what the host tree does on every refresh (hosts + folders, no secrets).
func BenchmarkStoreList500(b *testing.B) {
	s := benchStore(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cs, err := s.Connections()
		if err != nil || len(cs) != benchHosts {
			b.Fatal(len(cs), err)
		}
		fs, err := s.Folders()
		if err != nil || len(fs) != benchFolders {
			b.Fatal(len(fs), err)
		}
	}
}

// BenchmarkStoreGetConnection: open one host for connecting (includes decrypting its secret).
func BenchmarkStoreGetConnection(b *testing.B) {
	s := benchStore(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, err := s.Connection(fmt.Sprintf("host-%03d", i%benchHosts))
		if err != nil || c.Secret == "" {
			b.Fatal(c.Secret, err)
		}
	}
}

// BenchmarkHostSearch500: one keystroke of the sidebar search over 500 hosts / 50 folders. The predicate is
// a copy of appui's App.matches/folderSearchText (kept here because appui is a UI package), so this tracks
// the algorithm, not the widget tree.
func BenchmarkHostSearch500(b *testing.B) {
	hosts, folders := benchHostList(), benchFolderList()
	folderName := func(id string) string {
		for _, f := range folders {
			if f.ID == id {
				return f.Name
			}
		}
		return ""
	}
	for _, q := range []string{"", "prod-web-4", "rack-3", "不存在"} {
		b.Run(fmt.Sprintf("q=%q", q), func(b *testing.B) {
			q := strings.ToLower(strings.TrimSpace(q))
			for i := 0; i < b.N; i++ {
				n := 0
				for _, c := range hosts {
					if q == "" || strings.Contains(strings.ToLower(c.Name+" "+c.Host+" "+c.User+" "+c.Notes+" "+strings.Join(c.Tags, " ")+" "+folderName(c.FolderID)), q) {
						n++
					}
				}
				if q == "" && n != benchHosts {
					b.Fatal(n)
				}
			}
		})
	}
}
