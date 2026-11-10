# NextShell 2 — Phase 1 native minimum

Independent Go module. 1.x remains specification only; no Electron/React code is reused. Native UI is pinned to **mygo v0.8.3**; pure-Go SQLite to **modernc.org/sqlite v1.44.3**. All work is local on `v2`.

```sh
cd v2
go build ./...
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go run ./cmd/nextshell                # separate new profile
# disposable native smoke:
go run ./cmd/nextshell --db :memory: --smoke
go run ./cmd/probe terminal
go run ./cmd/probe
```

From repository root, use `go -C v2 build ./...` / `go -C v2 test ./...`.

## Structure and boundaries

- `cmd/nextshell`: native window composition and guarded close lifecycle; `cmd/probe`: reproducible framework checks.
- `internal/model`, `security`, `storage`, `sshclient`: UI-independent core (checked with `go list -deps`, no mygo imports).
- `internal/appui`: native layout and injected `Platform` contract. It holds native terminal presentation handles; connections, management I/O, workbench sends, screen reads and application exit go through Platform.
- `internal/desktop`: system implementation: database, key-file reading, SSH, native terminal creation, asynchronous prompts, session lifetime. No frontend IPC.

## Phase 1 features

Integrated native titlebar quick connect (`user@host[:port]`, IPv6 `[address]:port`), directory sidebar/search, tabs (drag/reorder/rename/clone/reconnect/switcher and screen-text previews), bottom command workbench with bounded persistent history, right activity bar and close confirmation.

SSH supports password, encrypted/unencrypted private keys, Unix SSH agent and keyboard-interactive challenges; explicit first-use host trust and fail-closed changed fingerprints; keepalive with bounded response timeout; SOCKS4/4a and SOCKS5 with optional authentication; UTF-8, GBK/GB18030, Big5 and Shift-JIS. Local terminals use the mygo terminal plugin/PTY.

Connections have folder assignment, edit/duplicate, table/grid layouts, batch authentication binding and recycle/restore/permanent delete. Folder moves enforce cycles, depth and sibling names; referenced folders/keys/proxies cannot be deleted. Keys support paste/file import, Ed25519/RSA generation, rename and delete; proxies support create/edit/delete.

## Profile and safety

The default profile is OS config dir `NextShell-v2/nextshell.db`. Do not use a 1.x DB as `--db`: foreign/legacy profiles are rejected without schema changes. New device key is stored in the DB; credentials are encrypted with per-record AAD. Key/connection/proxy listing omits secrets. This is not a defense against theft of the whole DB/device.

On first SSH connection independently verify the displayed fingerprint, then type `TRUST`. Changed pins fail closed. Clipboard reads and program-originated clipboard writes are disabled. Command history is local plaintext (UI warns about this): do not put passwords in commands. Key generation/private-key file import happen in Platform.

## Explicit deferrals / limits

OSC 133 is **deferred, waiting on upstream** by alex's decision. `PromptEvent` / `Event.ShellMark` are reserved and never emitted; capabilities report false. No custom stream observer. See `../docs/v2/PHASE0.md`.

Chinese IME, hardware GPU, Windows/macOS real machines, tray and multi-window remain **UNVERIFIED**. Linux native single-window/software rendering and local protocol servers are tested, not a claim of cross-platform daily-use acceptance. SSH agent currently uses Unix sockets, not Windows named-pipe/Pageant integration. Keys are generated unencrypted inside the device-encrypted vault; encrypted key import is supported.

Legacy keychain fallback is an explicit unsupported Platform stub. 1.x DB migration, SFTP, remote editing, monitoring, command library and import/export/FinalShell are Phase 2 and have NOT started. Cloud workspaces/sync and shell event integration are later phases. There is no distribution/signing/update pipeline for v2 yet.

Testing covers first-party 1.x fingerprint/key-material cases and a deterministic Node scrypt/AES-GCM vector. `internal/security/testdata/generate-vector.mjs` reproduces the fixture from 1.x constants; it never uses real credentials.
