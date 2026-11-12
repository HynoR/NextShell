package agent

import (
	"path"
	"regexp"
	"strings"
)

// Verdict is the policy's view of a command line. There are three tiers:
//
//	Forbidden: never runs, whatever the user would say (disks, the root filesystem, power, …).
//	Risky / Sensitive: only runs after a manual approval in the app, even under auto-approve.
//	Sensitive additionally needs allowSensitive, the agent's statement that the user agreed.
//	neither: may be auto-approved.
//
// The classifier is a policy aid built on a small shell tokenizer (shell.go), not a sandbox: it
// does not expand variables or globs and cannot see what a script or a program does.
type Verdict struct {
	Forbidden, Risky, Sensitive bool
	Why                         []string // short reasons, shown in the approval prompt
}

// Manual reports whether auto-approve must not cover the command.
func (v Verdict) Manual() bool { return v.Risky || v.Sensitive }

// Tier is "sensitive", "risky" or "" for the approval prompt.
func (v Verdict) Tier() string {
	switch {
	case v.Sensitive:
		return "sensitive"
	case v.Risky:
		return "risky"
	}
	return ""
}
func (v *Verdict) forbid(why string) { v.Forbidden = true; v.note(why) }
func (v *Verdict) risk(why string)   { v.Risky = true; v.note(why) }
func (v *Verdict) sense(why string)  { v.Sensitive = true; v.note(why) }
func (v *Verdict) note(why string) {
	for _, w := range v.Why {
		if w == why {
			return
		}
	}
	if len(v.Why) < 6 {
		v.Why = append(v.Why, why)
	}
}
func (v *Verdict) merge(o Verdict) {
	v.Forbidden = v.Forbidden || o.Forbidden
	v.Risky = v.Risky || o.Risky
	v.Sensitive = v.Sensitive || o.Sensitive
	for _, w := range o.Why {
		v.note(w)
	}
}

// Classify judges one command line. blacklist holds the user's lines: plain text matches
// case-insensitively anywhere, "re:" lines are regular expressions; a hit is Forbidden.
func Classify(command string, blacklist []string) Verdict {
	var v Verdict
	lower := strings.ToLower(strings.NewReplacer("'", "", "\"", "", "\\", "").Replace(command))
	if strings.Contains(lower, ":(){") || strings.Contains(lower, ": () {") {
		v.forbid("fork bomb")
	}
	analyze(command, 0, &v)
	for _, p := range blacklist {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "re:") {
			if re, e := regexp.Compile(strings.TrimPrefix(p, "re:")); e == nil && re.MatchString(command) {
				v.forbid("matches the blacklist")
			}
		} else if strings.Contains(strings.ToLower(command), strings.ToLower(p)) {
			v.forbid("matches the blacklist")
		}
	}
	return v
}

// Dangerous reports tier 1: commands that never run.
func Dangerous(command string, blacklist []string) bool {
	return Classify(command, blacklist).Forbidden
}

// Sensitive reports commands that touch credentials or dump the environment.
func Sensitive(s string) bool { return Classify(s, nil).Sensitive }

// Risky reports tier 2: commands that always need a manual approval.
func Risky(s string) bool { return Classify(s, nil).Risky }

// ---- path helpers

func norm(arg, cwd string) string {
	a := strings.ReplaceAll(arg, "${HOME}", "$HOME")
	if a == "$HOME" || strings.HasPrefix(a, "$HOME/") {
		a = "~" + a[len("$HOME"):]
	}
	switch {
	case a == "~" || strings.HasPrefix(a, "~/") || strings.HasPrefix(a, "/"):
		return path.Clean(a)
	case cwd != "":
		return path.Join(cwd, a)
	}
	return path.Clean(a)
}

func rootish(p string) bool { return p == "/" || p == "/*" }
func wipeTarget(p string) bool {
	switch p {
	case "/", "/*", "~", "~/*", "/home", "/home/*":
		return true
	}
	return false
}
func under(p string, dirs ...string) bool {
	for _, d := range dirs {
		if p == d || strings.HasPrefix(p, d+"/") {
			return true
		}
	}
	return false
}
func isDevice(p string) bool {
	if !strings.HasPrefix(p, "/dev/") {
		return false
	}
	switch p {
	case "/dev/null", "/dev/stdout", "/dev/stderr":
		return false
	}
	return !under(p, "/dev/fd", "/dev/shm")
}

func hasShort(arg string, letters string) bool {
	return strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsAny(arg[1:], letters)
}
func nonFlags(args []string) []string {
	var out []string
	rest := false
	for _, a := range args {
		if rest || !strings.HasPrefix(a, "-") || a == "-" {
			out = append(out, a)
		} else if a == "--" {
			rest = true
		}
	}
	return out
}
func has(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n {
				return true
			}
		}
	}
	return false
}
func quoteJoin(words []string) string {
	q := make([]string, len(words))
	for i, w := range words {
		q[i] = "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
	}
	return strings.Join(q, " ")
}

// ---- resolving what runs

var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\+?=`)
var reserved = map[string]bool{"if": true, "then": true, "else": true, "elif": true, "do": true, "while": true, "until": true, "{": true, "}": true, "!": true, "fi": true, "done": true, "time": true}

type invocation struct {
	name string // base name of what runs; "" for assignments only
	args []string
	priv bool // through sudo, doas or su
	env  bool // went through env
}

func skipFlags(words []string, withArg ...string) []string {
	for len(words) > 0 && strings.HasPrefix(words[0], "-") && words[0] != "-" {
		f := words[0]
		words = words[1:]
		if f == "--" {
			break
		}
		if len(words) > 0 && has(withArg, f) {
			words = words[1:]
		}
	}
	return words
}

// resolve strips env assignments, reserved words and wrapper commands (sudo, env, nice, nohup, time,
// command, xargs, …) to find the command that actually runs.
func resolve(words []string) invocation {
	var inv invocation
	for len(words) > 0 {
		w := words[0]
		base := path.Base(w)
		switch {
		case assignment.MatchString(w) || reserved[w]:
			words = words[1:]
			continue
		case base == "sudo" || base == "doas":
			inv.priv = true
			words = skipFlags(words[1:], "-u", "-g", "-h", "-p", "-C", "-r", "-t", "-T", "-U", "-D", "-R", "--user", "--group", "--host", "--prompt")
			continue
		case base == "env":
			inv.env = true
			words = skipFlags(words[1:], "-u", "-C", "-S", "--unset", "--chdir", "--split-string")
			continue
		case base == "command" || base == "builtin":
			words = words[1:]
			if len(words) > 0 && (words[0] == "-v" || words[0] == "-V") {
				return invocation{} // a lookup, not an execution
			}
			words = skipFlags(words)
			continue
		case base == "nice":
			words = skipFlags(words[1:], "-n", "--adjustment")
			continue
		case base == "ionice":
			words = skipFlags(words[1:], "-c", "-n", "-p", "-P", "-u", "--class", "--classdata")
			continue
		case base == "stdbuf":
			words = skipFlags(words[1:], "-i", "-o", "-e")
			continue
		case base == "timeout":
			words = skipFlags(words[1:], "-s", "-k", "--signal", "--kill-after")
			if len(words) > 0 {
				words = words[1:] // the duration
			}
			continue
		case base == "xargs":
			words = skipFlags(words[1:], "-I", "-n", "-P", "-d", "-E", "-L", "-s", "-a", "-J", "-R", "-S")
			continue
		case base == "nohup" || base == "exec" || base == "busybox" || base == "setsid" || base == "unbuffer":
			words = skipFlags(words[1:])
			continue
		}
		inv.name, inv.args = base, words[1:]
		if base == "su" {
			inv.priv = true
		}
		return inv
	}
	return inv
}

// ---- tier 1 and tier 2

var (
	sensitiveBase   = regexp.MustCompile(`(?i)^(?:\.env.*|\.netrc|_netrc|\.pgpass|\.npmrc|\.pypirc|\.git-credentials|id_(?:rsa|dsa|ecdsa|ed25519)(?:[^.].*)?|id_(?:rsa|dsa|ecdsa|ed25519)\.(?:pem|key))$`)
	redirPrefix     = regexp.MustCompile(`^[0-9&]*[<>]+&?`)
	interpreter     = regexp.MustCompile(`^(?:sh|bash|zsh|dash|ksh|ash|csh|tcsh|fish|python[0-9.]*|perl[0-9.]*|ruby[0-9.]*|node|nodejs|php[0-9.]*|lua[0-9.]*|pwsh|powershell)$`)
	shellName       = regexp.MustCompile(`^(?:sh|bash|zsh|dash|ksh|ash|csh|tcsh|fish)$`)
	packageManagers = map[string]bool{"apt": true, "apt-get": true, "aptitude": true, "yum": true, "dnf": true, "zypper": true, "apk": true, "pacman": true, "brew": true}
	pacmanChange    = regexp.MustCompile(`^-[SRU]`)
	pacmanQuery     = regexp.MustCompile(`^-S[sil]`)
	packageVerbs    = []string{"install", "remove", "purge", "upgrade", "dist-upgrade", "full-upgrade", "autoremove", "autopurge", "erase", "reinstall", "uninstall", "add", "del"}
)

// sensitivePath reports whether a word names a credential file.
func sensitivePath(w string) bool {
	w = redirPrefix.ReplaceAllString(w, "")
	if i := strings.LastIndexByte(w, '='); i >= 0 {
		w = w[i+1:]
	}
	if w == "" {
		return false
	}
	p := strings.ReplaceAll(w, "${HOME}", "$HOME")
	base := path.Base(p)
	slash := "/" + strings.TrimPrefix(path.Clean(p), "/")
	switch {
	case sensitiveBase.MatchString(base):
		return true
	case strings.HasSuffix(slash, "/.aws/credentials"), strings.HasSuffix(slash, "/.kube/config"), strings.HasSuffix(slash, "/.docker/config.json"):
		return true
	case slash == "/etc/shadow" || slash == "/etc/shadow-" || slash == "/etc/gshadow":
		return true
	}
	if i := strings.LastIndex(slash, "/.ssh/"); i >= 0 {
		rest := slash[i+len("/.ssh/"):]
		file := rest
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			file = rest[:j]
		}
		public := strings.HasSuffix(rest, ".pub") || strings.HasPrefix(file, "known_hosts") || strings.HasPrefix(file, "authorized_keys") || file == "config"
		return rest != "" && !public
	}
	return false
}

func checkWrite(p string, v *Verdict) {
	switch {
	case p == "/etc/passwd" || p == "/etc/shadow":
		v.forbid("overwrites /etc/passwd or /etc/shadow")
	case isDevice(p):
		v.forbid("writes to a device")
	case p == "/proc/sysrq-trigger":
		v.forbid("triggers sysrq")
	case under(p, "/etc", "/boot", "/usr", "/var/lib"):
		v.risk("modifies a system directory")
	}
}

func analyze(script string, depth int, v *Verdict) {
	if depth > maxSubDepth {
		return
	}
	segs := tokenize(script)
	cwd := ""
	for i, seg := range segs {
		args, redirs := splitRedirects(seg.words)
		stdin := append([]string{}, seg.stdin...)
		for _, r := range redirs {
			if r.op == "<<<" {
				stdin = append(stdin, r.target)
			}
			if r.writes() {
				checkWrite(norm(r.target, cwd), v)
			}
			if sensitivePath(r.target) {
				v.sense("reads or writes a credential file")
			}
		}
		for _, w := range args {
			if sensitivePath(w) {
				v.sense("reads or writes a credential file")
			}
		}
		for _, sub := range seg.subs {
			analyze(sub, depth+1, v)
		}
		inv := resolve(args)
		if inv.priv {
			v.risk("runs with elevated privileges")
		}
		if inv.name == "" {
			if inv.env {
				v.sense("dumps the environment")
			}
			continue
		}
		next := ""
		if i+1 < len(segs) {
			next = segs[i+1].sep
		}
		cwd = checkCommand(inv, seg, stdin, next, cwd, depth, v)
	}
}

// checkCommand applies the per-command rules and returns the working directory that follows it.
func checkCommand(inv invocation, seg segment, stdin []string, nextSep, cwd string, depth int, v *Verdict) string {
	name, args := inv.name, inv.args
	targets := nonFlags(args)
	switch {
	case name == "cd" || name == "pushd":
		switch {
		case len(targets) == 0 || targets[0] == "~" || targets[0] == "$HOME" || targets[0] == "${HOME}":
			return "~"
		case targets[0] == "-":
			return ""
		}
		return norm(targets[0], cwd)
	case strings.HasPrefix(name, "mkfs") || name == "mke2fs":
		v.forbid("formats a filesystem")
	case name == "poweroff" || name == "halt" || name == "reboot" || name == "shutdown":
		v.forbid("shuts down or reboots the host")
	case (name == "init" || name == "telinit") && (has(args, "0") || has(args, "6")):
		v.forbid("shuts down or reboots the host")
	case name == "systemctl" && (has(args, "poweroff", "reboot", "halt", "kexec")):
		v.forbid("shuts down or reboots the host")
	case name == "systemctl" && has(args, "stop", "disable", "mask"):
		v.risk("stops or disables a service")
	case name == "service" && has(args, "stop"):
		v.risk("stops a service")
	case name == "killall5":
		v.forbid("kills every process")
	case name == "kill" && has(args, "-1") && (has(args, "-9", "-KILL", "-SIGKILL", "-kill") || killsWith(args)):
		v.forbid("kills every process")
	case (name == "pkill" || name == "killall") && (has(args, "-9", "-KILL", "-SIGKILL") || killsWith(args)) && rootUser(args):
		v.forbid("kills every root process")
	case name == "shred" || name == "wipefs":
		for _, t := range targets {
			if isDevice(norm(t, cwd)) {
				v.forbid("wipes a device")
			}
		}
		for _, t := range targets {
			checkWrite(norm(t, cwd), v)
		}
	case name == "dd":
		for _, a := range args {
			if o, ok := strings.CutPrefix(a, "of="); ok {
				checkWrite(norm(o, cwd), v)
			}
		}
	case name == "rm":
		recursive := false
		for _, a := range args {
			recursive = recursive || a == "--recursive" || hasShort(a, "rR")
		}
		for _, t := range targets {
			p := norm(t, cwd)
			if recursive && wipeTarget(p) {
				v.forbid("recursive delete of the root or home directory")
			}
			checkWrite(p, v)
		}
		if recursive {
			v.risk("recursive delete")
		}
	case name == "chmod" || name == "chown" || name == "chgrp":
		recursive := has(args, "--recursive")
		for _, a := range args {
			recursive = recursive || hasShort(a, "R")
		}
		for _, t := range targets {
			p := norm(t, cwd)
			if recursive && rootish(p) {
				v.forbid("recursive permission change of /")
			}
			checkWrite(p, v)
		}
	case name == "mv":
		for i, t := range targets {
			p := norm(t, cwd)
			if i < len(targets)-1 && rootish(p) {
				v.forbid("moves the root filesystem")
			}
			checkWrite(p, v)
		}
	case name == "tee" || name == "truncate" || name == "touch" || name == "mkdir" || name == "rmdir" || name == "unlink" || name == "patch":
		for _, t := range targets {
			checkWrite(norm(t, cwd), v)
		}
	case name == "sed" && (has(args, "--in-place") || sedInPlace(args)):
		for _, t := range targets {
			checkWrite(norm(t, cwd), v)
		}
	case (name == "cp" || name == "install" || name == "ln" || name == "rsync") && len(targets) > 0:
		checkWrite(norm(targets[len(targets)-1], cwd), v)
	case name == "find":
		checkFind(args, cwd, depth, v)
	case packageManagers[name]:
		managed := has(args, packageVerbs...)
		if name == "pacman" {
			managed = false
			for _, a := range args {
				managed = managed || (pacmanChange.MatchString(a) && !pacmanQuery.MatchString(a))
			}
		}
		if managed {
			v.risk("installs or removes packages")
		}
	case (name == "dpkg" && has(args, "-i", "--install", "-r", "--remove", "-P", "--purge")) || (name == "rpm" && has(args, "-i", "-U", "-e", "--install", "--upgrade", "--erase")):
		v.risk("installs or removes packages")
	case name == "iptables" || name == "ip6tables" || name == "nft" || name == "ufw" || name == "firewall-cmd":
		readOnly := (strings.HasPrefix(name, "ip") && has(args, "-L", "-S", "--list", "-C")) || (name == "nft" && has(args, "list")) || (name == "ufw" && has(args, "status")) || (name == "firewall-cmd" && has(args, "--state", "--list-all"))
		if !readOnly {
			v.risk("changes the firewall")
		}
	case name == "crontab":
		for _, a := range args {
			if hasShort(a, "r") {
				v.risk("removes the crontab")
			}
		}
	case name == "userdel" || name == "deluser" || name == "passwd" || name == "chpasswd":
		v.risk("changes user accounts")
	case name == "base64" && nextSep == "|" && decodes(args):
		v.risk("decodes data into another command")
	case name == "eval":
		v.risk("eval")
		analyze(strings.Join(args, " "), depth+1, v)
	case name == "source" || name == ".":
		if seg.procsub {
			v.risk("sources generated code")
		}
	case name == "printenv" && len(args) == 0, name == "set" && len(args) == 0, name == "export" && (len(args) == 0 || (len(args) == 1 && args[0] == "-p")), (name == "declare" || name == "typeset") && (len(targets) == 0):
		v.sense("dumps the environment")
	}
	if interpreter.MatchString(name) {
		script := false
		if shellName.MatchString(name) {
			for i, a := range args {
				if hasShort(a, "c") && i+1 < len(args) {
					analyze(args[i+1], depth+1, v)
					script = true
					break
				}
			}
			for _, body := range stdin {
				analyze(body, depth+1, v)
			}
		}
		if seg.sep == "|" || len(stdin) > 0 || seg.procsub || (script && seg.subst) {
			v.risk("runs piped or generated code")
		}
	}
	if name == "su" {
		for i, a := range args {
			if (a == "-c" || a == "--command") && i+1 < len(args) {
				analyze(args[i+1], depth+1, v)
			}
		}
	}
	return cwd
}

func sedInPlace(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-i") || (hasShort(a, "i") && strings.Trim(a[1:], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") == "") {
			return true
		}
	}
	return false
}

func decodes(args []string) bool {
	for _, a := range args {
		if a == "--decode" || hasShort(a, "dD") {
			return true
		}
	}
	return false
}

// killsWith recognises "-s KILL", "-s 9", "--signal=KILL" style options.
func killsWith(args []string) bool {
	for i, a := range args {
		if (a == "-s" || a == "--signal") && i+1 < len(args) && (args[i+1] == "9" || strings.EqualFold(strings.TrimPrefix(strings.ToUpper(args[i+1]), "SIG"), "KILL")) {
			return true
		}
		if a == "--signal=9" || strings.EqualFold(a, "--signal=KILL") || strings.EqualFold(a, "--signal=SIGKILL") {
			return true
		}
	}
	return false
}
func rootUser(args []string) bool {
	for i, a := range args {
		switch a {
		case "-u", "-U", "--uid", "--euid":
			if i+1 < len(args) && (args[i+1] == "root" || args[i+1] == "0") {
				return true
			}
		case "-uroot", "-u0", "-Uroot", "-U0", "--uid=root", "--euid=root", "--uid=0", "--euid=0":
			return true
		}
	}
	return false
}

func checkFind(args []string, cwd string, depth int, v *Verdict) {
	var starts []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") || a == "(" || a == "!" {
			break
		}
		starts = append(starts, a)
	}
	if len(starts) == 0 {
		starts = []string{"."}
	}
	remove := false
	for i, a := range args {
		switch a {
		case "-delete":
			remove = true
		case "-exec", "-execdir", "-ok", "-okdir":
			end := i + 1
			for end < len(args) && args[end] != ";" && args[end] != "+" {
				end++
			}
			if i+1 < end {
				switch path.Base(args[i+1]) {
				case "rm", "shred", "unlink":
					remove = true
				}
				analyze(quoteJoin(args[i+1:end]), depth+1, v)
			}
		}
	}
	if !remove {
		return
	}
	for _, s := range starts {
		if rootish(norm(s, cwd)) {
			v.forbid("find deleting from /")
			return
		}
	}
	v.risk("find deleting files")
}
