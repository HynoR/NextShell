# NextShell 2 performance baselines

Rerun everything, no `make` (from `v2/`; Docker running; first run builds the images, ~1 min):

```sh
GOTOOLCHAIN=auto go test -tags interop -run '^$' -bench . -benchtime=3x -timeout 30m ./internal/interop/ && GOTOOLCHAIN=auto go test -run '^$' -bench . -benchmem ./internal/storage ./internal/sftpfs ./internal/transfer
```

Interop (correctness) tests: `GOTOOLCHAIN=auto go test -tags interop ./internal/interop/ -count=1 -timeout 30m`.
Details, images and users: `v2/testdata/interop/README.md`.

Use `-benchtime=3x` for the docker ones (each op moves 100-300 MB). Run on an idle machine: a concurrent build
or test run halves the numbers (we saw 2x swings from exactly that). `BenchmarkSmallFiles` takes ~1 min at 20 ms RTT.

## Machine

| | |
| --- | --- |
| Host | Apple M4, 10 cores, 24 GB, macOS 27.2.1 (arm64) |
| Docker | OrbStack 2.2.4, Docker 29.8.2; VM 10 CPUs / 12 GB, kernel 7.2.9-orbstack |
| Go | 1.27.3 (`GOTOOLCHAIN=auto`) |
| Servers | Debian 13 (OpenSSH 10.0p2), Ubuntu 22.04 (8.9p1), Alpine 3.22 (10.0p1); loopback through OrbStack's port forwarder |
| Date | 2026-11-12 |

The client and the server share one machine, so network is free and crypto/CPU is what you see. Throughput
numbers are "client + OpenSSH + OrbStack forwarder", not a LAN. Where it matters, a delaying TCP proxy
(`tcpProxy` in `internal/interop/resilience_test.go`) adds latency.

## Results

### Storage: 500 hosts, 50 folders (SQLite file, secrets encrypted) — `internal/storage`, no docker

| Benchmark | Time/op | Notes |
| --- | --- | --- |
| `StoreImport500Hosts` | 250 ms | 50 folders + 500 hosts into an empty profile (first run / restore): 0.45 ms per record |
| `StoreSaveConnection` | 0.47 ms | edit and save one host, secret re-encrypted |
| `StoreList500` | 0.96 ms | `Connections()` + `Folders()`, what a tree refresh costs; 1.2 MB / 7k allocs |
| `StoreGetConnection` | 22 µs | one host + secret decrypt |
| `HostSearch500` empty query | 3.8 µs | |
| `HostSearch500` with a query | 140-165 µs | 1000 allocs (the predicate builds one lowercase string per host per keystroke) |

Nothing here approaches a frame (16 ms). Search is the only allocation-heavy path; fine at 500, would want a
precomputed lowercase haystack past a few thousand hosts. The search predicate is a copy of appui's
`App.matches` (appui is a UI package; see the comment on the benchmark).

### SSH session setup — `BenchmarkDial` (TCP + key exchange + auth + PTY + shell)

password 36 ms, ed25519 key 37 ms, RSA-2048 key 37 ms.

### SFTP against OpenSSH (Debian) — `internal/interop`

| Benchmark | Result | OpenSSH CLI, same container |
| --- | --- | --- |
| `SFTPList10000` (10,000 files) | 33 ms (0.3 M entries/s) | |
| `SFTPUpload100MB` (`sftpfs.Upload`) | 593 MB/s | `sftp put`: 234 MB/s |
| `SFTPDownload100MB` | 703 MB/s | `sftp get`: 179 MB/s |
| `ExecChannel200MB` (`Session.Run`, no PTY) | 876 MB/s | `ssh host head -c`: 530 MB/s |

In-process baseline (Go server on loopback, no OpenSSH, `internal/sftpfs`): upload 844 MB/s, download 1006 MB/s,
list 10,000 files 35 ms. So the client is not the bottleneck; OpenSSH's sftp-server is.

### SFTP latency — `BenchmarkSFTPLatency` (16 MiB, delaying proxy in front of sshd)

| RTT | Upload before / after the pipelining fix | Download before / after |
| --- | --- | --- |
| 0 | 181 / 459 MB/s | 314 / 565 MB/s |
| 20 ms | **1.4 / 46 MB/s** | **5.5 / 53 MB/s** |
| 100 ms | **0.31 / 10.1 MB/s** | **1.2 / 11.5 MB/s** |

Before the fix `sftpfs` uploaded one 32 KiB write per round trip and downloaded one 128 KiB read per round
trip (32 KiB / RTT and 128 KiB / RTT: exactly what was measured). See "Findings" 1.

### Packed (tar.gz over an exec channel) — 100 files x 1 MiB

| Benchmark | Result |
| --- | --- |
| `PackedUpload` random data | 214 MB/s |
| `PackedUpload` text | 258 MB/s |
| `PackedDownload` random data | **50 MB/s** (remote `tar -czf` is the limit: gzip -6 of incompressible data runs at ~48 MB/s here, same through the OpenSSH CLI) |
| `PackedDownload` text | 242 MB/s |
| client only, no network (`internal/transfer`): `Pack` / `Extract` | 1.8 GB/s / 1.2 GB/s |

### Many small files — `BenchmarkSmallFiles` (100 files x 4 KiB)

| | per-file `sftpfs.Upload` | `UploadPacked` |
| --- | --- | --- |
| RTT 0 | 1.1 ms/file | 0.19 ms/file |
| RTT 20 ms | **182 ms/file** (9 round trips per file) | 0.8 ms/file |

### PTY output through the session reader — `BenchmarkSessionOutput200MB` (no terminal emulator attached)

| Remote output | Result | OpenSSH CLI `ssh -tt` |
| --- | --- | --- |
| 200 MB of 80-column lines (log/`cat`-like), UTF-8 | 157 MB/s | 156 MB/s |
| same, GBK decoder in the path | 152 MB/s | n/a |
| 200 MB of `yes` (a newline every 2 bytes; 300 MB on the wire) | 26 MB/s | 15 MB/s (200 MB payload / 13.5 s) |

This measures `Session.Read` into a 64 KiB buffer. Rendering (the mygo terminal widget) is not included: the
roadmap's "long `tail -f`" check still needs the real app.

## Findings

1. **SFTP was round-trip bound (fixed in this branch).** `UploadReader`/`Download` looped `Write`/`Read` with
   128 KiB buffers; pkg/sftp's plain `Write` sends 32 KiB and waits for the ack each time, and `Read` waits per
   buffer. Fine on loopback (130 MB/s), unusable over a WAN (1.4 MB/s at 20 ms). Now uses
   `ReadFromWithConcurrency` / `WriteTo` (up to 64 requests in flight). Guarded by
   `sftpfs.TestTransfersAreNotRoundTripBound` (runs without docker).
2. **Per-file SFTP uploads cost ~9 round trips each** (stat, open, write, close, chmod, stat, link, remove-temp
   for the atomic publish): 182 ms/file at 20 ms RTT, so 1,000 files take 3 minutes. Packed transfer is 200x
   faster there. `CopyTree` (server-to-server) has the same per-file cost plus the source side. Not changed, it
   is the price of never exposing partial files; the lever is running files concurrently in the transfer queue
   or sending the chmod with the open, and routing directory uploads through packed transfer when the remote
   supports it.
3. **Packed download of incompressible data is gzip-bound on the remote** (50 MB/s). `tar -czf` uses gzip -6;
   for already-compressed trees (photos, archives) `gzip -1` or no compression would be 100+ MB/s. Not a
   defect; worth a "don't compress" switch if users move media over packed.
4. **PTY output with a newline every 2 bytes (`yes`) is 20-25 MB/s regardless of client**: the remote kernel's
   tty line discipline (`\n` to `\r\n`) is the limit; OpenSSH's own client is no faster. Realistic text runs at
   ~155 MB/s. Not a product issue.
5. No pathology found in storage, listing 10,000 files (33 ms), dial time, or the session reader.
