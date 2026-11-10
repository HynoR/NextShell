# 阶段 0 验证记录（否决项阻塞）

验证时间：2026-11-10 20:00–20:03 Asia/Singapore (UTC+8)。
环境：Linux amd64，Go 1.27.3，DISPLAY=:17。仅使用本仓库 1.x 规格与 mygo 自身文档/API，未阅读任何同类第三方 SSH 客户端代码。

## 已锁定

- v2 独立 Go module：`v2/`，`github.com/egoist/mygo v0.8.3`，go.sum 已保存。
- 原生 UI，不使用 webview。核心 model/security/storage/sshclient 不导入 UI；appui 仅经 Platform 请求系统功能。
- pure-Go modernc.org/sqlite，全新 profile 默认 `NextShell-v2/nextshell.db`；设备密钥在 app_settings 中。
- 老 keytar `service=NextShell` / `account=device-key` 读取仅预留 Platform 方法，当前显式返回未实现错误；不会读取实际钥匙串、不会删除老密钥。阶段 2 迁移器未开始。

## 结果

|项目|结论|证据 / 限制|
|---|---|---|
|Linux 编译|PASS|`cd v2 && go build ./...`，退出 0|
|Linux 原生窗口生命周期|PASS（有限）|`go run ./cmd/probe` 创建窗口并在 3 秒后退出，输出 `native window lifecycle OK`|
|Linux 原生终端布局启动|PASS（仅启动）|`go run ./cmd/nextshell --db :memory: --smoke` 退出 0；未手动交互验收|
|GPU 渲染|UNVERIFIED|mygo 输出 `native UI draws without the GPU: there is none`，本次是软件渲染，不能冒充 GPU 通过|
|无窗口终端 + OSC 7 + 屏幕文本|PASS|`go run ./cmd/probe terminal` 输出 `dir="/tmp" text="probe-ok\n[Process exited]"`；libghostty-vt 自动下载并校验缓存|
|OSC 133 事件公开 API|**BLOCKED / 否决项**|v0.8.3 没有公开 OnOSC / OnPrompt / OSC133 事件订阅；内部消费标记不等于应用可拿到事件，见下|
|中文 IME（终端、输入框）|UNVERIFIED|无中文输入法实机交互验证|
|Windows / macOS 真机|UNVERIFIED|此 Linux box 不具备这些真机|
|托盘、多窗口实际渲染|UNVERIFIED|尚未做真机验证|

GTK 输出 AT-SPI accessibility bus 缺失警告，未阻止本次窗口生命周期；无障碍功能未验证。

## OSC 133 阻塞证据

固定版本源码：`github.com/egoist/mygo@v0.8.3/plugins/terminal/terminal.go`。

- 第 206–209 行公开回调为 OnTitle / OnExit / OnBell / OnNotify，无 OSC 133 回调。
- 第 842 行 `t.prompt.feed(p)` 在插件内部处理 prompt markers；prompt 状态和 tracker 未导出。
- 第 932 / 1048 行公开 `Dir()` 和 `Text()`，因此 OSC 7 当前目录及屏幕文本可轮询读取，但不提供命令开始/结束、退出状态等 OSC 133 事件。
- 文档提及 OSC 133 对光标移动/重绘的支持，不代表将事件暴露给应用。

按计划“任何一项不过，回到路线选择重新评估”，在此停止，没有自行改框架、修改 mygo 或绕过该门槛继续阶段 1/2。

## 需 alex 选择（仅建议，未执行）

1. 保持要求严格：先让 mygo 提供公开 OSC 133 事件 API，再锁版本继续。
2. 经明确批准后，允许在自有 io.ReadWriteCloser 层自研 OSC 7/133 字节流观察器（SSH 可行；本地 PTY 亦需由 Platform 自己持有，不能仅用插件内建 Command），并补分包/跨读边界/转义序列测试。这是对“由插件拿事件”的实现调整，不应擅自当作通过。

无任何外部 issue、消息、推送或 PR。
