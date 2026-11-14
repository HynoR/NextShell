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
# macOS 上会跳过把本机当 Linux 远端的测试(打包传输、监控探针等),Windows 上还会跳过所有把本机当 POSIX 远端的测试(远端 exec、SFTP 目录树、shell 钩子),合并前用 Docker 在 Linux 里补跑全套:
docker run --rm -v "$PWD":/src -w /src -v nextshell-gomod:/go/pkg/mod -e GOTOOLCHAIN=auto golang:1.27 go test ./... -count=1
# 真实 OpenSSH 互操作测试(Docker):
go test -tags interop ./internal/interop/ -count=1 -timeout 30m
# 会打开窗口的端到端测试(默认跳过):SIGTERM 在会话打开时无需确认即退出
NEXTSHELL_GUI_TESTS=1 go test ./cmd/nextshell -run SIGTERM -count=1
```

Agent 测试:`go run -tags nextshell_devtools ./cmd/nextshell --agent-test --db <临时目录>/t.db [--import hosts.json]` 启动即开启 MCP 和自动批准,并在 stderr 打印 `mcp.json` 路径(必须带 `--db`;默认和发布构建没有这个参数)。`--import` 把明文 `nextshell-connections` v1 文件导入测试 profile,主机指纹不会预先信任;加 `--trust-new-hosts` 则首次见到的主机指纹自动信任(指纹变化仍会询问)。这三个参数只存在于 `nextshell_devtools` 构建。

开发版 inspector:`go run -tags nextshell_devtools ./cmd/nextshell`,并设置 `NEXTSHELL_DEVTOOLS=1`,Ctrl+Alt+F12 打开。默认构建和发布构建都关闭。

## 结构

- `cmd/nextshell`:窗口组装与退出流程。`cmd/probe`:框架能力探针。
- 核心(不依赖 UI,有架构测试守护):`model`、`security`、`storage`、`sshclient`、`sftpfs`、`transfer`、`remoteedit`、`monitor`、`exchange`、`cloudv2`、`cloudsync`、`cloudhttp`、`agent`、`mcpendpoint`、`diagnostics`、`commandtemplate`、`shellintegration`、`tabswitch`。
- `internal/desktop`:`Platform` 的系统实现(数据库、SSH、终端、文件监视等)。
- `internal/appui`:原生界面,只通过 `Platform` 接触系统。正在按 DESIGN.md 重做。

## 安全要点

- 设备密钥存在同一数据库,凭据 AES-256-GCM 加密、资源 ID 作 AAD;不防整库被盗。
- 主机指纹首次需确认,变化即拒绝。终端剪贴板读写默认关闭。命令历史为本地明文。
- MCP 端点 `127.0.0.1:41777/mcp` 默认关闭(开启后会记住,下次启动同一 profile 时自动开启;自动批准仍不记住),开启后需 bearer token,每次变更需应用内批准。开启期间应用把 `{"port","token"}` 写入 `mcp.json`(权限 0600;默认 profile 在 `<系统配置目录>/NextShell-v2/`,`--db` 指定的 profile 写在该数据库旁),关闭或退出时删除;`@nextshell/mcp` 自动读取它。
- 自动批准(15 分钟 / 1 小时 / 直到退出)仅存内存,紧急停止、关闭 MCP、重启都会关闭。命令分三级:禁止(永不执行:清盘/删根或家目录/关机重启/`kill -9 -1`/改写 passwd 等,加上用户黑名单)、风险(`sudo`、`rm -r`、管道进 shell、写 `/etc` 等、包管理、服务停止、防火墙:始终手动批准,自动批准也不覆盖)、其余(可自动批准)。凭据文件(`.env*`、ssh 私钥、`.aws/credentials` 等)和环境变量导出需 `allowSensitive` 并手动批准。输出会脱敏(Authorization/Cookie、`key=value`、常见令牌格式、PEM/PGP 私钥、`sshpass -p`)。命令策略防的是**误操作**,不是蓄意或被提示词注入的 Agent:shell 总有办法绕过任何分类器。自动批准不是安全沙箱:只在你信任当前 Agent 任务时开启。
- Agent 不打扰用户:每次运行第一次变更只提示并前置窗口一次(不抢键盘),之后不前置窗口、不切标签、不抢焦点;Agent 打开的会话在后台并带标记;`session_focus` 是显式交还(60 秒内最多前置一次);用户 3 秒内在该标签按过键时 Agent 不会往里输入(`user_typing`)。

## 已知限制

- 监控、打包传输要求远端是 Linux(`/proc`、GNU coreutils);对应测试在非 Linux 上跳过。
- OSC 133 依赖 mygo 上游开放 prompt 事件,暂未实现。
- 云同步只有测试用参考服务端。
- SSH agent 只支持 Unix socket(暂不支持 Windows Pageant / 命名管道)。
- Windows 没有 POSIX 权限位:导出文件、数据库、`mcp.json` 的 0600 只在 macOS/Linux 上由权限位保证,Windows 上靠所在目录(用户配置目录)继承的 ACL,应用不另设 ACL。
