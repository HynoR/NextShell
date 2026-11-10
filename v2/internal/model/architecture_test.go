package model_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestArchitectureCoreAndPlatformBoundary(t *testing.T) {
	root := ".."
	core := []string{"model", "security", "storage", "sshclient", "exchange", "migrate", "sftpfs", "transfer", "remoteedit", "monitor"}
	check := func(pkg string, ui bool) {
		entries, e := os.ReadDir(filepath.Join(root, pkg))
		if e != nil {
			t.Fatal(e)
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			f, e := parser.ParseFile(token.NewFileSet(), filepath.Join(root, pkg, entry.Name()), nil, 0)
			if e != nil {
				t.Fatal(e)
			}
			for _, im := range f.Imports {
				path, _ := strconv.Unquote(im.Path.Value)
				if !ui && (strings.Contains(path, "egoist/mygo") || strings.HasSuffix(path, "/appui") || strings.HasSuffix(path, "/desktop")) {
					t.Error("core imported UI", pkg, path)
				}
				if ui && (path == "os" || path == "net" || strings.HasPrefix(path, "os/") || strings.HasPrefix(path, "net/") || strings.HasPrefix(path, "syscall") || strings.HasPrefix(path, "golang.org/x/sys")) {
					t.Error("UI imported system I/O", path)
				}
			}
			if ui {
				ast.Inspect(f, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					ident, ok := sel.X.(*ast.Ident)
					if ok && (ident.Name == "remoteedit" || ident.Name == "sftpfs" || ident.Name == "transfer" || ident.Name == "monitor") {
						t.Error("UI invoked core I/O instead of Platform", ident.Name, sel.Sel.Name)
					}
					return true
				})
			}
		}
	}
	for _, pkg := range core {
		check(pkg, false)
	}
	check("appui", true)
}
