# NextShell 2 — native Phase 1 + Phase 2 + Phase 2.5

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
- `internal/model`, `security`, `storage`, `sshclient`, `exchange`, `cloudv2`, `sftpfs`, `transfer`, `remoteedit`, `monitor`: UI-independent core. An AST regression test forbids core UI imports and appui system-I/O imports/calls.
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

There is NO v1 database/device-key/keychain migration or fallback, and no archive API. Only user-exported v1 connection files may be imported. Real cloud networking and shell event integration remain later phases. There is no distribution/signing/update pipeline for v2 yet.

## Phase 2 workflows

Use the right activity bar: **文件 / 传输 / 远程编辑 / 监控 / 命令库 / 导入导出**. All I/O goes through Platform.

- SFTP: browse/up/OSC-7-follow, upload/download, create/rename/remove, native file-drop handler, streamed cross-server send. Transfers have bounded concurrency (2), progress, cancellation and explicit retry. Regular-file replacement uses the server POSIX-rename extension; unsupported atomic overwrite fails without deleting the original.
- Packed directory transfer: tar.gz streams over an independent SSH exec channel. Remote Linux needs `tar` and GNU `mv -T -n`; destination must be new, not merged. Download extraction rejects traversal/links/special files, enforces size/entry limits and verifies gzip integrity.
- Remote editor: UTF-8 regular text up to 8 MiB; hash conflict detection and atomic SFTP save. External executable + JSON argv array (e.g. `["--wait"]`, no shell interpolation), watched auto-return. Successful sessions remove private staging; errors preserve a private recovery file and report its path. Read that message and remove recovery copies yourself when no longer needed.
- Linux monitoring uses a separate bounded exec probe: CPU deltas, memory/swap, aggregate non-loopback network rates, disks, system info, process and SS/netstat panels. TERM/KILL/STOP/CONT require UI confirmation. Missing tools/permissions produce warnings, not fabricated metrics.
- Saved commands: CRUD/grouping and current/all-session sends (explicit all-session confirmation). Commands/history are local plaintext; do not save credentials there. Template metadata is preserved, not expanded.
- Data: preview then atomic import; preserve/reuse directory chains; default skip, rename-copy (new UUID/name), or explicit confirmed overwrite of a unique local endpoint. Previews identify target IDs and become invalid after profile edits. Overwrite preserves target identity, absent passwords and fields not in the file; an explicit empty password clears it. FinalShell single JSON/directory import, unsigned/signed Java-Random DES password decoding and missing-key rebinding. Encrypted `b64##` `nextshell-connections` v1 exports are 1.x-compatible; plain exports omit passwords, exports never include private keys. Export refuses existing output files.
- No v1 profile upgrade: export a connection file from v1, preview/import it in v2. No old DB, device key, keychain, archive or migration UI. Import accepts v1 encrypted/plain non-XOR files; contradictory omitted-password flags are rejected. Key fingerprints must match when supplied; unresolved key references are retained for rebinding.

## Phase 2.5 offline foundation

Own schema 3 internally upgrades ONLY existing v2 profiles, preserving UUIDs, device key and secrets. Resource scope, UTC millisecond timestamps, revision, sort/key metadata, command append-CR, keepalive tri-state, deletion ledger, workspace vault and trusted sync heads are independent of v1. File imports target local scope only; folder/key/proxy references cannot cross scopes. A missing device key with existing secrets fails closed. Historic opaque settings are left untouched, not migrated/restored.

`internal/cloudv2` is a new **version=2 offline contract**, NOT old cloud sync v3. It does NOT interoperate with v1 clients/workspaces. Independent random auth token/verifier; separate passphrase-derived KEK wraps a random DEK; whole snapshots (metadata, commands and secrets) use AES-256-GCM. Whole-workspace CAS + local dirty/head/revision checks prevent implicit overwrite. No HTTP client/login/background upload or production server exists here. See [offline security contract](../docs/v2/CLOUD-V2-OFFLINE.md). Independent review, recovery and actual networking are Phase 3 work.

The v1 connection exchange remains version 1, not a complete v2 backup. IDs/timestamps/proxies/private keys/commands/empty folders/trust/workspace metadata are absent by design. Shift-JIS cannot be represented and is refused for compatible export. Actual first-party v1 TS crypto/parser are exercised in both directions (Node 24 required for that integration test); the unrelated crypto-js dependency is a throwing stub, never used by those NextShell-only tests. FinalShell retains its independent first-party vector tests.

Limits: no online cloud activation or recycle/preference/template restoration UI; no arbitrary non-UTF-8 text editing; no resume-from-byte-offset transfers. Remote edit is optimistic hash checking, not remote CAS (another writer can race between check and rename). A dropped connection/cancelled SFTP channel can leave a uniquely named `.nextshell-*.part` remotely; cleanup is best effort. Packed/local directory publication and non-extension SFTP rename cannot promise a cross-platform atomic no-replace against a concurrent writer. Network rates aggregate interfaces; there is no interface selector or chart history yet. Native drop/external desktop-editor UX still needs real desktop acceptance.

See `../docs/v2/PHASE2.5-PLAN.md` and `../docs/v2/PHASE2.5-REPORT.md` for the current boundary; Phase 1/2 reports are historical.

Testing covers first-party 1.x fingerprint/key-material, folder/export/parser specs, deterministic Node crypto/FinalShell vectors and real loopback SSH/SFTP servers. `internal/security/testdata/generate-vector.mjs` reproduces the fixture from 1.x constants; it never uses real credentials.
