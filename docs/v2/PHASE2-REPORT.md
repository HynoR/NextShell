# 阶段 2 报告

> 历史验收记录：阶段 2.5 已按 alex 决定移除 v1 DB 迁移器、keychain stub、迁移 UI 和归档逻辑；旧测试数/功能描述不代表当前版本。当前边界见 PHASE2.5-REPORT.md。

2026-11-10 SGT（Asia/Singapore，UTC+8）。本地 `/workspace/NextShell`、分支 `v2`；未推送、未开 PR；阶段 3 未开始。

## 完成及提交

- **`d3accfd`**：只读 1.x SQLite 迁移器，优先读 `app_settings.value_json` 设备密钥；本地连接/密钥/代理/空目录/命令/历史原子迁入、重新加密、幂等及回滚；既有 v2 数据保留。加密 v1 导出/导入、FinalShell DES 解密及对应 Node 向量。
- **`2d261ae`**：SFTP 浏览/文件操作/上传下载/跨服务器流传输，队列取消重试与并发限制；tar.gz 打包传输及安全解包；内置/外部远程编辑；Linux 系统、进程、网络探针。
- **`00e2da7`**：六个原生活动页接入 Platform，OSC 7 目录跟随、系统拖入 handler、进程与全部会话命令确认、数据预览/导入/导出；真实 SSH/SFTP 端到端测试、填充数据的 headless UI 测试、架构边界测试、文档与进一步输入校验。

原有未知修改仍保留在 **`stash@{0}: pre-existing-uncommitted-before-v2`**，未恢复、未丢弃、未混入 v2。分支继续基于既定 origin/main `3c5e5a8`。mygo 仍精确固定 v0.8.3，modernc SQLite v1.44.3，无框架替换；本阶段未读取第三方同类客户端。

## 实测结果

环境：Go **1.27.3 linux/amd64**，Node **v24.18.1**。最终命令均在 `v2/` 执行，无缓存测试（`-count=1`）：

| 命令 | 真实结果 |
|---|---|
| `go build ./...` | exit 0，无错误输出 |
| `go vet ./...` | exit 0，无诊断 |
| `go test ./... -json -cover -count=1 -timeout=120s` | exit 0；**85 顶层测试、181 pass 事件（含子测试）、163 叶子用例；0 失败、0 跳过** |
| `go test -race ./... -json -count=1 -timeout=120s` | exit 0；相同 **85 / 181 / 163**，无 race 报告 |
| `go run ./cmd/nextshell --db :memory: --smoke`（18 秒外部限时） | DISPLAY=:17，exit 0，原生窗口及本地终端启动后自行退出；有 AT-SPI bus 缺失及无 GPU 软件渲染提示 |
| headless 原生编辑器截图测试 | exit 0，截图已查看，六个新增页渲染测试通过 |

**12 个含测试包**；另有 2 个 cmd 和 1 个测试辅助包无独立测试。关键覆盖率：迁移 **83.1%**、交换 **74.0%**、SFTP **76.7%**、队列/打包 **86.9%**、远程编辑 **78.0%**、监控 **93.8%**、appui **76.1%**、desktop **70.6%**。这些是包级覆盖率，不是完整 1.x 功能覆盖率。

证据保存在 `/workspace/NextShell-notes/`：`phase2-build-vet.log`、`phase2-go-test.json`、`phase2-go-race.json`、`phase2-test-summary.json`、`phase2-native-smoke.log`、`phase2-native-editor.png`。

真实测试包括 loopback Go SSH/SFTP 服务端、实际文件与独立 exec 通道、tar 上下行、双服务端发送、外部编辑器进程运行中回传、冲突恢复副本、只读 SQLite 前后字节对比与失败事务回滚。仅给测试自建 sleep 进程发过信号，没有连接用户真实远端。Node 固定向量验证 scrypt/AES-GCM 信封及 v1 payload 的逐字节 crypto 互操作、FinalShell unsigned/signed 两条解密路径；并非声称所有 JSON 表示都拥有相同规范化字节。移植的是所列关键 1.x 测试规格，不是全部 134 个测试文件。

## UNVERIFIED / 偏差与缺口

- **UNVERIFIED**：中文 IME、Windows/macOS 真机、硬件 GPU、托盘/多窗口、系统拖入及真实 GUI 外部编辑器 UX。Linux 软件窗口 / headless Unicode / 自建编辑器测试不代表这些通过。
- **OSC 133：deferred, waiting on upstream**。仍无自有流观察器，没有更改这一决定；未发现新的路线否决阻塞。
- keytar `NextShell` / `device-key` fallback 仍为 Platform 明确 stub；旧库无 device_key 时不能恢复其密文，迁移会报错而非伪造成功。
- 迁移保留查询白名单表的**加密 JSON 快照，非完整数据库备份**。云数据、偏好、回收站、模板参数只归档，不激活；有核心读取 API，暂无恢复 UI。旧文件路径私钥需重绑。保留源库。
- 导入仅 skip / duplicate，无 overwrite；XOR 路径按计划删除。命令库不展开模板参数；命令/历史为本地明文。
- 打包需要远端 Linux tar + GNU mv，目标必须新建；无断点续传。断网/取消可能残留唯一 `.part`，清理尽力而为；非扩展 SFTP rename / 本地目录发布不保证面对并发写入的跨平台原子 no-replace。
- 编辑限 UTF-8、8 MiB；hash 检查不是远端 CAS，仍有检查到 rename 的竞争窗口。外部编辑器需等待退出；失败保留私密明文恢复文件，提示用户取回后自行清理。
- 监控仅远端 Linux、网络聚合排除 lo；无接口选择/历史图表。缺少 ss/netstat 或权限不足明确提示，不显示假数据。

详细范围和复现步骤：`docs/v2/PHASE2-CHECKLIST.md`、`v2/README.md`、`docs/v2/PHASE0.md`。到此停止，等待下一阶段指示。
