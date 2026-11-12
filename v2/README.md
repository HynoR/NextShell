# NextShell 2

全新的原生 SSH 客户端:Go + [mygo](https://github.com/egoist/mygo) v0.9.0 原生 UI(非 webview),纯 Go SQLite。不迁移 1.x 数据;1.x 用户导出连接文件后在 2 中导入。

- 路线与现状:[`docs/v2/ROADMAP.md`](../docs/v2/ROADMAP.md)
- 界面设计:[`docs/v2/DESIGN.md`](../docs/v2/DESIGN.md)
- 云同步协议:[`docs/v2/CLOUD-V2-OFFLINE.md`](../docs/v2/CLOUD-V2-OFFLINE.md)、[`docs/v2/CLOUD-V2-PROTOCOL.md`](../docs/v2/CLOUD-V2-PROTOCOL.md)
- 依赖审计:[`docs/v2/GO-ECOSYSTEM-AUDIT.md`](../docs/v2/GO-ECOSYSTEM-AUDIT.md)

```sh
cd v2
go run ./cmd/nextshell                     # 默认 profile:<系统配置目录>/NextShell-v2/nextshell.db
go run ./cmd/nextshell --db :memory:       # 一次性 profile
go vet ./... && go test ./... && go test -race ./...
# macOS / Windows 上会跳过把本机当 Linux 远端的测试(打包传输、监控探针等),合并前用 Docker 在 Linux 里补跑全套:
docker run --rm -v "$PWD":/src -w /src -v nextshell-gomod:/go/pkg/mod -e GOTOOLCHAIN=auto golang:1.27 go test ./... -count=1
# 真实 OpenSSH 互操作测试(Docker):
go test -tags interop ./internal/interop/ -count=1 -timeout 30m
```

Agent 测试:`go run -tags nextshell_devtools ./cmd/nextshell --agent-test --db <临时目录>/t.db` 启动即开启 MCP 和自动批准,并在 stderr 打印 `mcp.json` 路径(必须带 `--db`;默认和发布构建没有这个参数)。

开发版 inspector:`go run -tags nextshell_devtools ./cmd/nextshell`,并设置 `NEXTSHELL_DEVTOOLS=1`,Ctrl+Alt+F12 打开。默认构建和发布构建都关闭。

## 结构

- `cmd/nextshell`:窗口组装与退出流程。`cmd/probe`:框架能力探针。
- 核心(不依赖 UI,有架构测试守护):`model`、`security`、`storage`、`sshclient`、`sftpfs`、`transfer`、`remoteedit`、`monitor`、`exchange`、`cloudv2`、`cloudsync`、`cloudhttp`、`agent`、`mcpendpoint`、`diagnostics`、`commandtemplate`、`shellintegration`、`tabswitch`。
- `internal/desktop`:`Platform` 的系统实现(数据库、SSH、终端、文件监视等)。
- `internal/appui`:原生界面,只通过 `Platform` 接触系统。正在按 DESIGN.md 重做。

## 安全要点

- 设备密钥存在同一数据库,凭据 AES-256-GCM 加密、资源 ID 作 AAD;不防整库被盗。
- 主机指纹首次需确认,变化即拒绝。终端剪贴板读写默认关闭。命令历史为本地明文。
- MCP 端点 `127.0.0.1:41777/mcp` 默认关闭,开启后需 bearer token,每次变更需应用内批准。开启期间应用把 `{"port","token"}` 写入 `mcp.json`(权限 0600;默认 profile 在 `<系统配置目录>/NextShell-v2/`,`--db` 指定的 profile 写在该数据库旁),关闭或退出时删除;`@nextshell/mcp` 自动读取它。
- 自动批准(15 分钟 / 1 小时 / 直到退出)仅存内存,紧急停止、关闭 MCP、重启都会关闭;风险命令、黑名单命中和 `.env` 访问不受影响。

## 已知限制

- 监控、打包传输要求远端是 Linux(`/proc`、GNU coreutils);对应测试在非 Linux 上跳过。
- OSC 133 依赖 mygo 上游开放 prompt 事件,暂未实现。
- 云同步只有测试用参考服务端。
- SSH agent 只支持 Unix socket(暂不支持 Windows Pageant / 命名管道)。
