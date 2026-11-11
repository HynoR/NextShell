// Command mygo-automation drives a running MyGo app through
// plugins/automation's socket, made for AI coding agents: compact text
// snapshots of a window's elements with short refs (@e1), actions on
// them, and diffs of what the window shows once it settled.
//
//	go install github.com/egoist/mygo/plugins/automation/cmd/mygo-automation
//	mygo-automation skills
//
// skills prints the guide, SKILL.md, embedded in the command so that it
// matches the commands of the version installed. See
// docs/plugins/automation.md.
package main

import (
	_ "embed"
	"os"

	"github.com/egoist/mygo/internal/automationcli"
)

//go:embed SKILL.md
var skill []byte

func main() {
	os.Exit(automationcli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, automationcli.Options{Skill: skill}))
}
