# NextShell 2 产品路线(MVP → 成品)

更新:2026-11-12。本文件取代 `docs/v2/` 下各阶段报告(PHASE*/V1-*/GUI-*/UI-*),那些报告保留在 git 历史中。

## 定位

NextShell 2 是**全新产品**,不是 1.x 的升级版。

- 不读取、不迁移 1.x 数据库、设备密钥、钥匙串。
- 与 1.x 的桥只有两座:① 1.x 导出连接文件 → 2 导入(已完成);② 可选:从云端一次性拉取部分数据(连接、命令片段)。
- 1.x 代码只当功能清单;界面和数据模型不照搬。**不再维护 v1 对照表(PARITY)的统计**,剩余差异按新设计判断要不要做。UI 文案里不出现 "v1"、"OSC"、"keepalive 三态" 这类实现术语。

## 现状(2026-11-12 评估)

| 层 | 状态 |
|---|---|
| 核心包(sshclient / sftpfs / transfer / storage / security / exchange / monitor / remoteedit / cloudv2 / agent / mcpendpoint) | 可用,测试充分,保留 |
| `internal/desktop`(Platform 实现) | 可用,保留 |
| `internal/appui`(约 5k 行) | **MVP 后台表单式界面**:全局上帝对象 `App`,每个功能是一整页表单,手工输入路径;切到文件/监控会挡住终端;右侧 17 个无标签图标。**按 DESIGN.md 重做** |
| 平台 | 只在 Linux 无头机开发过;macOS 能编译、能启动 |
| 框架 | mygo v0.9.0(已去掉 vendored fork) |

## 里程碑

### M0 清理 ✅

- [x] 去掉 `third_party/mygo` fork,升级 mygo v0.9.0
- [x] macOS 测试全绿:快捷键改用 `ui.Cmd`;依赖 Linux 远端的测试在非 Linux 上跳过
- [x] 文档收敛:删除阶段报告,只留 ROADMAP / DESIGN / CLOUD-V2 协议 / 生态审计
- [x] CI:`.github/workflows/v2.yml`(macOS + Linux + Windows,Linux 加 `-race`;桥测试)。推送前在 Mac 上用 README 里的 Docker 命令补跑 Linux

### M1 新外壳(UI 架构 + 主布局)✅ 2026-11-12

按 DESIGN.md 实现:

- `appui` 按功能拆文件,`App` 的表单状态拆进各功能的 state struct;页面路由改成「主视图 + 右侧面板 + 对话框」三类
- 主布局:标题栏(标签)/ 左侧主机栏 / 中间终端 / 右侧上下文面板 / 底部状态栏
- 欢迎页(无标签时)、命令面板(Cmd+K)、连接编辑对话框、设置窗口
- 标签操作全部收进右键菜单和快捷键,删除终端上方的工具条

验收:不看文档就能完成「新建主机 → 连接 → 开第二个标签 → 打开文件面板 → 上传一个文件」。

### M2 功能面板重做 ✅ 2026-11-12

- 文件面板:目录面包屑 + 虚拟化表格 + 右键菜单 + 拖入上传 / 拖出下载 + 原生文件选择对话框;不再有路径输入框
- 传输:状态栏进度 + 弹出的传输列表
- 监控面板:卡片 + 迷你图,进程 / 网络连接用二级标签
- 命令片段面板:搜索、点击发送、参数模板对话框
- 远程编辑:独立窗口,mygo 多窗口
- 并入原对照表剩余缺口:主机拖进目录 / 目录排序、回收站清空与多选还原、片段分组重命名删除、多选下载 / 删除、进程详情、关闭确认开关、Shell 集成自动 / 手动 / 关闭

M2 未做:拖出到 Finder 下载(mygo 只能拖出已存在的文件,需上游支持 file promise);目录拖拽排序(`SidebarSection` 不暴露标题,改用菜单上移 / 下移)。

### M3 macOS 日用验收(进行中)

- [x] 真实 OpenSSH 互操作 + 性能基准(`internal/interop`,`docs/v2/PERF.md`);修了 known_hosts 算法协商、SFTP 流水线(20ms RTT 下 1.4→46 MB/s)、打包传输报错、BusyBox 监控提示
- [ ] Agent 通过 MCP 驱动应用的测试(进行中)

- 你本人连续日用一周,问题进 issue
- 中文输入法(终端 + 输入框)、Cmd 快捷键、全屏 / 多显示器 / Retina
- 性能:500 主机列表、1 万文件目录、长时间 `tail -f`、20 个标签;启动时间、内存
- 崩溃与错误:本地日志文件、panic 恢复不丢数据
- 互操作:真实 OpenSSH sshd(含 PAM、keyboard-interactive、2FA)、vim / htop / tmux、zsh / fish
- 之后再找有 GPU 的 Linux 和 Windows 机器各验一轮

### M4 同步与 Agent

- 云同步:为 version=2 协议实现并部署真实服务端(目前只有测试用参考实现);新设备加入流程;恢复快照 UI
- 可选:1.x 云工作区一次性只读拉取(连接 + 命令),需要你确认 1.x 服务端的接口
- MCP:发布支持 token 的 `nextshell-mcp` 新版,更新 Claude Code 插件
- OSC 133:向 mygo 提 issue / PR 暴露 prompt 事件;不阻塞发布

### M5 发布

mygo 自带签名、公证、DMG、NSIS、更新(见 mygo `docs/distribution.md`、`updates.md`、`i18n.md`)。

- `mygo build` 配置、图标、Bundle ID、证书
- 自动更新通道(beta / stable)
- i18n:中文 + 英文,文案抽到资源
- 先出 macOS arm64 beta,再 Windows、Linux

### M6 新功能

端口转发、跳板机、分屏、会话录制。

## 待办池(未排期)

- SSH agent forwarding、ProxyJump(随 M6 跳板机)
- 跨服务器目录复制每个文件约 9 个往返,高延迟下慢;可改为两端都有 tar 时走打包流
- mygo 上游 issue:终端插件公开 OSC 133 事件;拖出文件支持 file promise;`SidebarSection` 标题可拖拽
- 应用被强杀时 `mcp.json` 残留(下次开启会覆盖)
- Windows:SSH agent 走命名管道 / Pageant;CI 的 Windows 任务尚未真实跑过

## 决策记录(2026-11-12)

- **范围**:本轮做到 M3,加上 M4 里的 MCP 部分(MCP 能让 agent 驱动应用,加速 M3 测试)。云同步服务端、OSC 133 不在本轮。
- **MCP token 发现**:应用开启 MCP 时写 `<系统配置目录>/NextShell-v2/mcp.json`(`{"port","token"}`,权限 0600),关闭时删除、轮换时重写。`nextshell-mcp` 按 `--token` > `NEXTSHELL_MCP_TOKEN` > 该文件的顺序取 token,用户无需手动配置。
- **自动批准**:Agent 设置里提供「自动批准」开关,时长可选 15 分钟 / 1 小时 / 直到退出;重启后一律关闭;风险命令和黑名单命中仍需手动批准;开启期间状态栏常驻提示。
- **Agent 测试模式**:仅 `nextshell_devtools` 构建提供 `--agent-test` 参数,启动即开启 MCP 和自动批准,配合 `--db` 使用临时 profile;`--import <文件>` 导入明文连接文件;发布构建不含这些参数。
- **命令策略三级**(2026-11-12 修订):禁止 / 风险(始终手动批准)/ 其余可自动批准;凭据文件与环境导出需要同意。Agent 不打扰用户:首次变更提示一次,之后后台工作,`session_focus` 为显式交还。
- **互操作测试**:用 Docker 起真实 OpenSSH 容器,测试放在 `interop` 构建标签下,默认 `go test` 不跑。

## 分工

- Opus(high):M1、M2 的界面大改。
- Sonnet(xhigh):MCP 与桥、CI、互操作和性能测试等通用任务。
- 主会话:规划、决策、合并、验收。

## 工作方式

- 规划和验收:主会话。机械性改动派子 agent,每次给清晰范围。
- 每个改动都要:`go vet`、`go test`、`go test -race` 在 macOS 上通过;界面改动附离屏截图(`ui.Tester.Image()`,明 / 暗两套)。
- 只在 `v2` 分支提交,不推送,除非明确要求。
