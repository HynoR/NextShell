# 阶段 1 报告：Linux 本地实现与自动化验收完成

> 历史验收记录：阶段 2.5 已按 alex 决定移除 v1 DB 迁移器、keychain stub、迁移 UI 和归档逻辑；旧测试数/功能描述不代表当前版本。当前边界见 PHASE2.5-REPORT.md。

更新：2026-11-10 20:27 Asia/Singapore (UTC+8)。本报告取代此前“阶段 0 阻塞停止”版本；alex 已批准 OSC 133 等待上游、其余阶段 1 继续。**阶段 2 未开始，现在停止交接。**

## 做了什么 / 本地提交

- `94f8165`：原计划已保存为 `docs/NEXTSHELL-2.0-PLAN.md`；v2 分支从 `origin/main` 的 `3c5e5a8` 创建。
- `1c7a58a`：独立 Go 核心、pure-Go SQLite/设备密钥/密文存储加固；真实本地 SSH 与 SOCKS 服务器测试，移植指纹/私钥与 scrypt/AES-GCM 规格。
- `e1f5550`：原生主布局及日用流程，头部快连、左侧目录、拖动标签、底部工作台、右侧活动栏；本地终端、连接/目录/密钥/代理管理与 headless UI 回归。
- `aeb5000`：目录引用、循环、同级重名、深度及并发移动保护；一致的资源/密文读取。
- `5367719`：工作台发送/预览/退出等系统能力经 Platform；增加密钥与代理界面回归，保留上游 OSC 133 事件接口。
- `ccb4aea`：批准延期记录、架构/运行说明、阶段 1 验收映射。
- 本报告为后续本地文档提交；最终交接给出其 hash。

mygo 精确固定 **v0.8.3**，SQLite **modernc.org/sqlite v1.44.3**；Go module 位于 `v2/`。未改 1.x 功能代码。核心 model/security/storage/sshclient 的依赖树无 mygo/UI；系统实现由 desktop Platform 提供。

## 已实现并验证的日用范围

- 原生集成标题栏快连；可搜索/折叠目录树；标签拖拽/排序/重命名/复制/关闭、成功后原位重连、切换器与屏幕文本预览；真实本地 PTY；命令按钮和 Enter 发送、最多 100 条本地持久历史；活动栏与关闭确认。
- SSH：密码、私钥/加密私钥、Unix agent、keyboard-interactive；首次指纹显式确认与持久 pin，拒绝变更；keepalive 响应/超时；握手/PTY setup 取消与超时；SOCKS4/4a/5（含认证、拒绝及取消）；UTF-8/GBK/GB18030/Big5/Shift-JIS，测试直接核对服务端线缆编码字节。
- 连接 CRUD/复制、table/grid、批量认证绑定、目录归属、回收/恢复/永久删除；目录重命名/移动及引用保护；密钥粘贴/文件导入、Ed25519/RSA 生成、改名/删除；代理 CRUD。
- 独立新 schema / profile，设备密钥在 DB，重启保持；凭据不以明文落盘，资源 AAD 防替换；列表不返回私密材料；旧库/未知库与损坏设备密钥拒绝处理。Node 确定性向量证明 scrypt 参数与 AES-256-GCM 信封字节互通。

详细映射：`docs/v2/PHASE1-CHECKLIST.md`。这里的“验收”是 Linux box 自动化路径，不代表 alex 已在实际生产服务器上真人长期日用验收。

## 真实构建 / 测试结果（最终代码）

在 `/workspace/NextShell/v2` 执行：

- `go build ./...`：退出 0。
- `go vet ./...`：退出 0。
- `go test ./... -json -cover -count=1 -timeout=90s`：退出 0，**51 个顶层测试；包含子测试共 118 个 pass 事件 / 104 个叶测试，0 失败**。6 个内部包有测试；2 个 cmd 入口无单测，另用实际运行验证。
- `go test -race ./... -json -count=1 -timeout=120s`：退出 0，6 个被测包全部通过，未报告数据竞争。
- 包覆盖率：model 97.1%、security 82.1%、storage 81.4%、sshclient 85.2%、appui 84.6%、desktop 65.4%。覆盖率不是功能完备或安全证明。
- `go list -deps`：4 个核心包无 mygo 依赖。
- `go run ./cmd/nextshell --db :memory: --smoke`：原生单窗口/集成标题栏/本地终端启动并正常退出，最终超时保护命令返回 0。开发中曾因关闭确认挡住 smoke 退出而超时，已修正 smoke 的显式退出路径并清理那次自建临时进程。
- `go run ./cmd/probe terminal`：`dir="/tmp" text="probe-ok\n[Process exited]"`；OSC 7 / 屏幕文本 PASS。
- `go run ./cmd/probe`：`native window lifecycle OK`。

原始结果（仓库外）：

- `/workspace/NextShell-notes/phase1-go-test.json`
- `/workspace/NextShell-notes/phase1-go-race.json`
- `/workspace/NextShell-notes/phase1-native-smoke.log`
- `/workspace/NextShell-notes/core-deps.txt`
- `/workspace/NextShell-notes/phase1-terminal.png`（headless 原生 UI 渲染截图，不是 GPU 或 IME 实机证明）

## OSC 133 / 限定参考检查

OSC 133 **deferred, waiting on upstream**：仅留 `PromptEvent` / `Event.ShellMark` 接口，capability=false，无事件生产者、无自研流观察器、无 mygo fork。

按 alex 后续的狭窄许可，参考仓库只克隆到 `/workspace/finallyshell-ref`，HEAD `ac2a1e85dab2fbc1616ed44165ff8a4d39896316`。针对 OSC 133 / prompt marks / 回调 hook 的检索没有找到可用简单方案；没有继续读无关实现、没有借用代码或其他设计。因此仍等待上游。详情及固定版本 API 证据见 `docs/v2/PHASE0.md`。

## UNVERIFIED / 偏离 / 已知缺口

- **UNVERIFIED**：中文 IME（输入框与终端）、Windows/macOS 真机、硬件 GPU、托盘、多窗口、真人实际服务器长期日用。box 使用软件渲染；GTK 有 AT-SPI bus 缺失警告。没有将这些记为 PASS，也没有做跨平台打包。
- alex 批准的偏离：OSC 133 延后，不能靠它记录命令边界/退出状态。命令历史来自工作台发送，不是自动捕获 shell 全历史。
- 老 keychain `NextShell` / `device-key` 读取仍是 Platform 显式 stub；无自动凭据恢复承诺。阶段 2 迁移前需实现或由迁移流程明确提示。
- agent 当前为 Unix socket；Windows named-pipe/Pageant 未实现。密钥生成为 vault 内无口令 OpenSSH 私钥（DB 内再加密）；支持带口令私钥导入。
- 批量认证逐连接提交，不是全批次原子事务；出错时已成功部分会保留。
- 命令历史本地明文，界面明确提示不要输入密码；设备密钥与密文同库的安全边界按计划保留。未做专业安全审计。
- 阶段 2 的 DB 迁移器、SFTP/队列、远程编辑、监控、进程/网络、命令库、导入导出/FinalShell 及对应移植测试均未开始；云工作区/同步、Agent/MCP、发布流程属于后续阶段。

## 工作树保护与操作边界

原来未知来源的删除和 3 个未跟踪脚本仍完整保存在 `stash@{0}`：`On main: pre-existing-uncommitted-before-v2`；没有混入 v2，没有丢弃。未 push、未开 PR、未提交任何外部 issue/消息。所有提交均只在本地。运行方式及 profile 边界见 `v2/README.md`。
