//go:build nextshell_devtools

package desktop

// TrustNewHosts makes connections pin the host key of a server seen for the first time without asking.
// A key that differs from a pinned one still prompts. It exists only in development builds, for agent
// tests that would otherwise wait for a dialog nobody answers; a release build has no way to turn it on.
func (p *Platform) TrustNewHosts(on bool) { p.trustNewHosts.Store(on) }
