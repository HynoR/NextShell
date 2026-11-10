# 阶段 1 报告：未完成，阶段 0 否决项触发后停止

时间：2026-11-10 20:04 Asia/Singapore (UTC+8)。

## 本地状态

- 按要求等待并读取 `/workspace/NextShell-notes/structure.md` 后才操作 git。
- 原目录是已有 checkout；未知来源的两项删除及三份未跟踪 smoke 脚本已完整保存在 `stash@{0}`：`On main: pre-existing-uncommitted-before-v2`，没有混入 v2、没有丢弃。
- `v2` 从 `origin/main` 的 `3c5e5a8` 创建，本地 main 未移动。未 push、未开 PR、无外部写操作。
- `94f8165`：保存原计划 `docs/NEXTSHELL-2.0-PLAN.md`。
- `ee1f371`：固定 mygo v0.8.3，独立 v2 Go module、阶段 0 证据及未验收的阶段 1 草稿。该提交误含 probe 构建产物，随本报告提交删除并加入 ignore；没有修改既有提交历史。
- 本报告所在后续本地提交：移除本次 probe 产物并记录停止点（最终交接附提交号）。

## 实际完成 / 未完成

已编译的草稿：原生主布局（快连、左侧栏、标签、底部命令工作台、右侧活动栏）、本地终端入口、连接/目录/密钥/代理表单，独立 SQLite/密文存储，以及密码/私钥/agent/keyboard-interactive、指纹提示、SOCKS4a/5、keepalive、编码 SSH 代码。核心不依赖 UI，系统操作由 Platform 实现。

**上述不是阶段 1 验收通过**：没有真实 SSH 测试服务器或自动化测试；没有验证每项管理操作；标签拖拽/多会话预览尚未实现；命令历史仅内存；旧 keychain fallback 是明确 stub；DB 身份保护、连接取消/keepalive 等边界待加固。请勿将这个 WIP 用于真实数据库或凭据。

## 真实执行结果

在 `v2/`：

- `go build ./...`：退出 0。
- `go test ./...`：退出 0；**8 个 package 均 `[no test files]`，测试用例 0，不是功能通过**。
- `go run ./cmd/probe terminal`：退出 0；`dir="/tmp" text="probe-ok\n[Process exited]"`，OSC 7 与 Text API 可读取。
- `go run ./cmd/probe`：退出 0；`native window lifecycle OK`。
- `go run ./cmd/nextshell --db :memory: --smoke`：退出 0；原生布局/本地终端启动路径可运行。
- mygo 提示无 GPU，实际为软件渲染；AT-SPI bus 缺失有警告。

## 阻塞与停止原因

**固定 mygo v0.8.3 的 terminal 插件没有公开 OSC 133 事件 API**。它内部消费标记，但只公开 Title/Dir/Text 等读取与 OnTitle/OnExit/OnBell/OnNotify 回调；没有命令开始/结束事件可订阅。详见 `PHASE0.md` 的固定版本文件与行号证据。

按计划“任何一项不过重新评估”及 VETO STOP 要求，发现后停止实现，没有擅自切框架、fork mygo 或写字节流观察器来冒充门槛通过。阶段 2 未开始。

## 未验证

中文 IME（终端/输入框）、Windows/macOS 真机、硬件 GPU、托盘、多窗口及实际日用 SSH 行为均 UNVERIFIED。

## 偏离 / 待决策

阶段 0 检查不够前置：先写了阶段 1 草稿，完整检查 OSC 133 公开 API 时才发现阻塞；已如实标记 WIP 而非完成。Phase 1 未达“日常使用”里程碑，测试移植尚未进行。

需 alex 决定：等待 mygo 提供公开事件 API；或明确批准自研流观察器（包括本地 PTY 接管）作为实现调整，补完测试后再继续。没有发外部 issue 或消息。
