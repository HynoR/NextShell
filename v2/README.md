# NextShell 2 native rewrite — WIP / STOPPED

This independent Go module leaves 1.x unchanged. **Not ready for daily use.**
Phase 0 is blocked on the pinned terminal plugin lacking a public OSC 133 event API. See `../docs/v2/PHASE0.md`. Do not start Phase 2 or claim Phase 1 is complete.

```sh
cd v2
go build ./...
go test ./...  # currently NO TEST FILES; not an acceptance result
go run ./cmd/probe terminal
go run ./cmd/probe
go run ./cmd/nextshell --db :memory: --smoke
# interactive, separate new profile:
go run ./cmd/nextshell
```

- `cmd/nextshell`: native window composition; `cmd/probe`: phase-zero reproducible checks.
- `internal/model`, `security`, `storage`, `sshclient`: UI-independent draft core.
- `internal/appui`: native views and injected Platform contract.
- `internal/desktop`: system composition, SQLite, key files, SSH, native terminal creation.
- mygo is pinned to **v0.8.3**, pure-Go SQLite to modernc.org/sqlite **v1.44.3**.
- Default DB lives in OS config dir `NextShell-v2/nextshell.db`; NEVER pass a 1.x DB to `--db`. Database schema identity protection still needs implementation before any real data use.
- Device key and encrypted credentials share the DB, matching the selected convenience model. This protects against plaintext accidental exposure, NOT compromise of the DB/device. No actual user credentials were read or connected to.
- Clipboard reads and program writes are disabled in created terminals.

## Draft, not validated

Draft includes layout, quick connect, tabs, command input/in-memory history, connection/folder/key/proxy forms, encrypted resource persistence, SSH password/key/agent/keyboard-interactive transport, fingerprint trust prompt, SOCKS4a/5 and encodings. These paths have only been compiled, not acceptance-tested. UI gesture parity (tab dragging/preview), schema guards, SSH handshake/cancellation/keepalive edge cases, and credential lifecycle hardening remain. No SSH/SFTP test server or Go test vectors have been implemented yet. No claim of security review or daily-use readiness.

Legacy keychain fallback is an explicit stub through Platform. Migration, FinalShell, export interop and their tests belong to the unstarted Phase 2. Chinese IME, Windows/macOS, hardware GPU, tray and multi-window are UNVERIFIED.
