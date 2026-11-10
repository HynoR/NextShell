# 阶段 0：本地验证与批准延期项

更新：2026-11-10 Asia/Singapore (UTC+8)。先前的 OSC 133 阻塞停止记录见 git 历史；alex 后续明确决定等待上游 API，并批准其余阶段 1 工作继续。

## 定型

- 同仓 `v2` 分支、独立 Go module `v2/`。mygo **v0.8.3** 精确固定，原生 UI；没有 webview 或框架替换。
- pure-Go **modernc.org/sqlite v1.44.3**，全新 schema；独立 OS 配置目录 `NextShell-v2/nextshell.db`。
- 设备密钥保存于同库 `app_settings.device_key`，凭据 AES-256-GCM 加密、资源标识作为 AAD。数据库与密钥同处只降低意外明文暴露，不防整个设备/数据库被窃。
- 以 SQLite application_id=0x4e534832 和 user_version=2 识别 v2；拒绝写入旧版或不认识的数据库。损坏设备密钥报错，不生成新密钥覆盖。
- 老 keytar `service=NextShell` / `account=device-key` 仅有 Platform 显式 stub；不会在新 profile 启动时读取钥匙串。阶段 2 的只读迁移器现已优先读取旧库 `app_settings.value_json` 中的设备密钥；无旧 keychain 回读能力时明确报错，不能冒充凭据已恢复。

## 验证状态

| 项目 | 状态 | 证据 / 限制 |
|---|---|---|
| Linux Go build | PASS | `cd v2 && go build ./...` |
| Linux 原生窗口生命周期 | PASS（有限） | `go run ./cmd/probe` 输出 `native window lifecycle OK` |
| 原生布局、本地终端启动 | PASS（有限） | `go run ./cmd/nextshell --db :memory: --smoke` 正常退出；真实 PTY 和 headless UI 有 Go 测试 |
| 硬件 GPU | UNVERIFIED | box 没有 GPU，mygo 使用软件渲染；不能当作 GPU 通过 |
| OSC 7 / 屏幕文本 | PASS | probe 输出 `dir="/tmp" text="probe-ok\n[Process exited]"`；真实本地终端 Go 测试再次核验 |
| OSC 133 事件 | **deferred, waiting on upstream** | alex 已明确批准延期；不实现自有流观察器 |
| 中文 IME：终端及输入框 | UNVERIFIED | headless Unicode 输入测试不是中文 IME 实机验证 |
| Windows / macOS 真机 | UNVERIFIED | 无对应真机 |
| 托盘、多窗口实际渲染 | UNVERIFIED | 本阶段只启动一个原生窗口，没有托盘/多窗口实机验收 |

GTK 有 AT-SPI accessibility bus 缺失警告，不阻止本次启动；无障碍支持未实机验收。

## OSC 133：证据与接口留白

固定 `github.com/egoist/mygo@v0.8.3/plugins/terminal/terminal.go`：

- 206–209 行公开 OnTitle / OnExit / OnBell / OnNotify，没有 OSC 133 事件订阅。
- 842 行 `t.prompt.feed(p)` 是插件内部消费，tracker 没有导出。
- 932 / 1048 行 `Dir()` 与 `Text()` 可读取 cwd / 屏幕，不等于有命令开始、结束或退出状态事件。

`internal/appui/platform.go` 预留 `PromptEvent` 与 `Event.ShellMark`；没有事件生产者。`Platform.Capabilities().OSC133` 为 false。**TODO(upstream-mygo)**：等待公开 API 后再实现，不解析、截取或观察原始终端字节流；不修改或 fork mygo。

## narrowly-authorized finallyshell 检查

alex 后续只放宽 OSC 133 这一项参考权限。依其许可将仓库克隆到 **仓库外** `/workspace/finallyshell-ref`，检查时 HEAD 为 `ac2a1e85dab2fbc1616ed44165ff8a4d39896316`。

只针对 `OSC 133` / `133;` / `OnPrompt` / `OnOSC` / shell integration prompt marks 做定向检索，**没有找到可用的 OSC 133 公开 API 或简单 hook**。未继续阅读无关实现、未借用任何代码或其他设计，也没有把参考仓库放入 NextShell。检索结果不是对整个项目“不可能有相关能力”的断言。按 alex 已批准的等待方案保持延期；本实现没有采用来自该项目的方案。

无外部 issue、评论、推送或 PR。

阶段 2 更新：未新增路线否决阻塞；未改动 mygo 固定版本、OSC 133 延期结论或上表 UNVERIFIED 项。本阶段未再次访问 finallyshell 或其他第三方 SSH 客户端代码。
