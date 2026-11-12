# Interop tests and benchmarks (real OpenSSH in Docker)

Behind the `interop` build tag, so a plain `go test ./...` never runs them. Needs a working `docker` CLI
(OrbStack / Docker Desktop); tests skip cleanly when the daemon is unreachable.

```sh
# one line, from v2/ — everything (tests, ~4 min cold: first run builds the images)
GOTOOLCHAIN=auto go test -tags interop ./internal/interop/ -count=1 -timeout 30m -v

# benchmarks (docker ones, then the storage/SFTP baselines that need no docker)
GOTOOLCHAIN=auto go test -tags interop -run '^$' -bench . -benchtime=3x -timeout 30m ./internal/interop/ && go test -run '^$' -bench . -benchmem ./internal/storage ./internal/sftpfs
```

Pick one area with `-run`, e.g. `-run 'TestAuthMatrix|TestKeyboard'`, `-run TestPTY`, `-run TestSFTP`,
`-run 'TestKeepalive|TestBlackhole|TestContainer'`, `-run TestSOCKS`, `-run 'TestHostKey|TestKnownHosts'`,
`-run TestPacked`, `-run TestMonitor`, `-run TestRemoteEdit`.

## What is here

| Dir | Image | Purpose |
| --- | --- | --- |
| `debian/` | Debian 13 (OpenSSH 10.x); also built as Ubuntu 22.04 (OpenSSH 8.9) via `BASE` | PAM, keyboard-interactive, 2FA (google-authenticator scratch codes), vim, htop, tmux, zsh, fish, iproute2, procps |
| `alpine/` | Alpine 3.22 | BusyBox userland, no PAM: shows what the product assumes of a remote |
| `socks5/` | Alpine + Dante | SOCKS5 on :1080 (no auth) and :1081 (`proxy`/`proxy-pass`), SOCKS4 on :1080 |

Users: `tester` / `test-password` (also the three generated public keys, passed in `AUTHORIZED_KEYS`),
`otpuser` / `test-password` + one-time code `11112222` (`33334444`, `55556666` spare; each works once per
container, `AuthenticationMethods keyboard-interactive`). Host keys are generated when a container starts, so
recreating a container on the same port presents a new key (host-key-changed tests rely on this).

Containers publish on `127.0.0.1:<random>`, are labelled `nextshell-interop=<run id>` and removed at the end
of the run (`TestMain`). If a run is killed: `docker ps -aq --filter label=nextshell-interop | xargs docker rm -f`.
Images are tagged `nextshell-interop/<name>:1`; Docker's layer cache makes rebuilds near-instant.

The Go side is `internal/interop` (`docker.go` is the fixture: `Start`, `Shared`, `Network`, `Term`).
Results and machine notes for the benchmarks: `docs/v2/PERF.md`.
