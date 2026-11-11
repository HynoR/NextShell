//go:build !mygo_noinspector

package ui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The link to where the app builds the element chosen opens that line in
// the developer's editor: the command MYGO_EDITOR names, else VISUAL's or
// EDITOR's, with the line passed as each editor expects, as tools for the
// web do. Without one, the file opens in the system's default application,
// at its top.

// openableSource notes whether a source location is a file here: a build
// with -trimpath has no such paths, and neither do apps run elsewhere.
type openableSource struct {
	source string
	ok     bool
}

// canOpenSource reports whether the file of a source location can open,
// looking only when the location changes: the panel is built every frame.
func (in *inspector) canOpenSource(source string) bool {
	if in.openable.source != source {
		file, _ := splitSource(source)
		ok := editorsRun && filepath.IsAbs(file)
		if ok {
			_, err := os.Stat(file)
			ok = err == nil
		}
		in.openable = openableSource{source, ok}
	}
	return in.openable.ok
}

// openSource opens a source location in the editor, and lists in the
// Issues tab what went wrong.
func (in *inspector) openSource(rt *engine, source string) {
	file, line := splitSource(source)
	if s := os.Getenv("MYGO_EDITOR"); needsTerminal(s) {
		rt.warn(fmt.Sprintf("MYGO_EDITOR is %q, which needs a terminal that an app doesn't have: name the command that opens one, with {file} and {line} where they go", s))
	}
	failed := func(what string, err error) {
		rt.warn(fmt.Sprintf("Couldn't open %s: %s: %v", shortSource(source), what, err))
	}
	setting := chooseEditor(os.Getenv)
	if setting == "" {
		rt.host.openURL(fileURL(file), func(err error) {
			if err != nil {
				failed("no editor is set in MYGO_EDITOR, and the default application failed", err)
				rt.requestFrame()
			}
		})
		return
	}
	name, args := editorCommand(setting, file, line)
	err := startEditor(name, args, func(err error) {
		if err != nil {
			rt.host.post(func() {
				failed(setting, err)
				rt.requestFrame()
			})
		}
	})
	if err != nil {
		failed(setting, err)
	}
}

// splitSource splits a location as callSite gives it, file:line. The file
// of a Windows path has a colon of its own.
func splitSource(source string) (file string, line int) {
	i := strings.LastIndexByte(source, ':')
	if i < 0 {
		return source, 0
	}
	line, err := strconv.Atoi(source[i+1:])
	if err != nil {
		return source, 0
	}
	return filepath.FromSlash(source[:i]), line
}

// fileURL returns the URL of an absolute path.
func fileURL(file string) string {
	p := filepath.ToSlash(file)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // C:/dir, on Windows
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// chooseEditor returns the command of the first of the variables that names
// an editor which can run here. EDITOR is often for a terminal, where git
// runs it, which an app that opens files on its own doesn't have.
func chooseEditor(getenv func(string) string) string {
	for _, name := range []string{"MYGO_EDITOR", "VISUAL", "EDITOR"} {
		if s := strings.TrimSpace(getenv(name)); s != "" && !needsTerminal(s) {
			return s
		}
	}
	return ""
}

// needsTerminal reports whether a command runs an editor that draws in a
// terminal. A command saying where the file goes knows what it runs, as one
// opening a window of a terminal for the editor.
func needsTerminal(setting string) bool {
	fields := splitCommand(setting)
	if len(fields) == 0 || slices.ContainsFunc(fields, isFileField) {
		return false
	}
	switch editorBase(fields[0]) {
	case "vi", "vim", "nvim", "view", "ex", "ed", "nano", "pico", "joe", "micro", "hx", "helix", "kak", "ne", "mcedit":
		return true
	case "emacs", "emacsclient":
		return slices.ContainsFunc(fields[1:], func(f string) bool {
			return f == "-nw" || f == "-t" || f == "--tty" || f == "--no-window-system"
		})
	}
	return false
}

func isFileField(f string) bool { return strings.Contains(f, "{file}") }

// editorCommand returns the command that opens a file at a line. A setting
// with {file} says where the file and {line} go, for what isn't known here.
func editorCommand(setting, file string, line int) (string, []string) {
	fields := splitCommand(setting)
	if slices.ContainsFunc(fields, isFileField) {
		r := strings.NewReplacer("{file}", file, "{line}", strconv.Itoa(line))
		for i, f := range fields {
			fields[i] = r.Replace(f)
		}
		return fields[0], fields[1:]
	}
	return fields[0], append(fields[1:], editorArgs(editorBase(fields[0]), file, line)...)
}

// editorBase returns the name of the program of a command, to tell editors
// by: the executables of Windows have extensions, and its scripts too.
func editorBase(program string) string {
	b := strings.ToLower(filepath.Base(program))
	for _, ext := range []string{".exe", ".cmd", ".bat"} {
		b = strings.TrimSuffix(b, ext)
	}
	return b
}

// editorArgs returns the arguments that open a file at a line in an editor,
// by its program's name. An editor not known gets the file alone: a line
// where it doesn't expect one could open the wrong file.
func editorArgs(editor, file string, line int) []string {
	l := strconv.Itoa(line)
	at := file + ":" + l + ":1"
	switch editor {
	case "code", "code-insiders", "codium", "vscodium", "cursor", "windsurf", "trae", "antigravity":
		return []string{"-r", "-g", at}
	case "subl", "sublime", "sublime_text", "atom", "wstorm", "charm", "zed":
		return []string{at}
	case "goland", "goland64", "idea", "idea64", "clion", "clion64", "rider", "rider64",
		"webstorm", "webstorm64", "pycharm", "pycharm64", "phpstorm", "phpstorm64", "rubymine", "rubymine64", "appcode":
		return []string{"--line", l, "--column", "1", file}
	case "mate", "rmate", "mine":
		return []string{"--line", l, file}
	case "gvim", "mvim", "joe":
		return []string{"+" + l, file}
	case "emacs", "emacsclient":
		return []string{"+" + l + ":1", file}
	case "notepad++":
		return []string{"-n" + l, "-c1", file}
	}
	return []string{file}
}

// splitCommand splits a command at its spaces, but not those in quotes, as
// a path may have. Backslashes stay, as Windows's paths have them.
func splitCommand(s string) []string {
	var (
		fields  []string
		b       strings.Builder
		quote   rune
		started bool
	)
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, started = r, true
		case r == ' ' || r == '\t':
			if started {
				fields = append(fields, b.String())
				b.Reset()
				started = false
			}
		default:
			b.WriteRune(r)
			started = true
		}
	}
	if started {
		fields = append(fields, b.String())
	}
	return fields
}
