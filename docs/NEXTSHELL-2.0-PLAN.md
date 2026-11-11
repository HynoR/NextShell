# NextShell 2.0 长期计划

> **阶段 2.5 优先修订（alex，2026-11-11 北京时间）**：本计划中旧 DB/keychain 迁移与旧 cloud v3 兼容条款作废。无 v1 自动升级；仅识别用户导出的 nextshell-connections v1 文件（加密/明文，拒绝 XOR）。云使用独立 version 2 协议，认证/加密密钥分离，元数据/命令全加密，不与 v1 同工作区互通；本阶段仅离线，联网阶段 3。执行细则见 [PHASE2.5-PLAN](./v2/PHASE2.5-PLAN.md)。下文旧条款保留为历史规格，不作为当前验收要求。

> 拟定于 2026-11-10。时长为单人加 AI 辅助的粗估,不是承诺。

## 目标

彻底抛弃 Electron,基于 [mygo](https://github.com/egoist/mygo) 从 0 重写 NextShell。不背 1.x 的历史包袱,只继承用户数据、对外协议和功能清单。

## 原则

- **完全自研**:代码、结构、设计都由我们自己从 0 写。第三方同类项目(如 finallyshell)只证明了"mygo 原生 UI 做 SSH 客户端"这条路线可行,**不作为参考,不阅读其代码来实现功能**。
- 1.x 的 renderer 只当布局与交互规格,1.x 的测试用例当移植规格。
- 核心包(ssh、存储、sftp、监控等)不依赖 UI;UI 只通过一个 Platform 接口接触系统。mygo 出问题时最坏只需换 UI 层。
- 锁定 mygo 版本,按需升级。

## 路线选择:mygo 原生 UI

mygo 同时支持原生 UI(Go 即时模式,GPU 绘制)和 webview。选原生 UI:

| | 原生 UI(采用) | webview |
|---|---|---|
| 1.x renderer(约 3.7 万行 React/antd) | 不能复用,只当规格 | 基本可复用 |
| IPC / 流控 / 单 xterm 回放等包袱 | 整体消失(单进程) | 原样带过来 |
| 体积 / 内存(mygo 官方数字) | 约 7MB / 约 44MB | 约 62MB,三平台 webview 行为不一 |

终端使用 mygo 的 terminal 插件(libghostty),SSH channel 直接作为 `io.ReadWriteCloser` 交给终端。mygo 允许同一应用混用两种窗口,个别界面实在难做时可单独用 webview 兜底。

## 1.x 资产盘点

### 复用

- **用户数据**:`<userData>/storage/nextshell.db`(SQLite),设备密钥在同库 `app_settings.device_key`。写一次性迁移器;老安装的密钥可能还在系统钥匙串(keytar,service `NextShell`,account `device-key`),需兜底读取。
- **协议与格式**:云同步 v3 接口(服务端不动)、连接导出文件格式(`nextshell-connections` v1)、scrypt + AES-256-GCM 信封。
- **Agent 生态**:MCP 工具 schema(`packages/shared/src/mcp-tools.ts`)、npm 上的 `nextshell-mcp` 桥(`packages/mcp`)、Claude Code 插件(`nextshell-plugin/`)。新版保持 `127.0.0.1:41777/mcp` 兼容即可不改。
- **Shell 集成脚本**(`apps/desktop/src/shared/shell-integration/*.{bash,zsh,fish,sh}`):直接 `go:embed`。
- **测试用例**(134 个文件):解析器、FinalShell 解密、加密信封的测试作为 Go 移植规格。

### 用 Go 重写(约 2.5 万行)

SSH 连接池、SFTP 与打包传输、本地终端、存储、监控探针与解析器、云同步客户端、MCP 网关、导入导出(含 FinalShell)、ping / nexttrace。

### 丢弃

118 个 IPC 通道与 Zod 契约、IPC 流控、单 xterm 回放与冻结帧、headless xterm 镜像、原生模块重编译、keytar、`group_path` 旧目录模型、云同步 v1/v2 残留、XOR 导出读取路径、`packages/ui-kit` 等死代码。

### 1.x 功能清单(移植范围)

- **终端**:SSH(密码 / 私钥 / agent / keyboard-interactive、主机指纹、keepalive、SOCKS4/5 代理、编码)、本地终端、标签页(拖拽、重命名、复制、重连、切换器、多会话预览)、命令输入栏与历史、快速连接。
- **Shell 集成 / OSC**:标题、cwd(OSC 7)、超链接、通知、进度、剪贴板(OSC 52)、OSC 133 标记。
- **SFTP**:浏览、上传下载(含打包)、传输队列、跟随终端目录、系统拖入、跨服务器发送、远程编辑(内置编辑器 / 外部编辑器自动回传)。
- **连接管理**:目录树、表格 / 网格视图、批量绑定认证、本地与云工作区作用域、SSH 密钥(导入 / 生成)、代理、回收站。
- **监控**:CPU / 内存 / 网络 / 磁盘、进程管理、网络连接、系统信息(远端仅 Linux)。
- **网络工具**:ping、traceroute(nexttrace)。
- **命令库**:保存命令、在当前或全部会话执行。
- **云同步**:工作区、端到端加密、命令同步。
- **Agent 网关**:MCP 服务、同意与黑名单策略、脱敏、活动面板、全局停止开关。
- **导入导出**:加密导出、FinalShell 导入。
- **设置与系统集成**:主题、托盘、关闭确认、背景图、更新检查。

## 阶段

### 阶段 0:验证与定型(约 1 周)

真机验证一票否决项:

- [ ] 中文输入法在终端和输入框里正常(mygo 有 CJK 排版的未关闭 issue)。
- [ ] 终端插件能拿到 OSC 7/133 事件和屏幕文本(跟随目录、Agent 的 `session_read` 与命令历史依赖它)。
- [ ] Windows、Linux 的实际表现(渲染、多窗口、托盘)。
- [ ] 定存储与密钥方案。倾向:纯 Go SQLite + 全新 schema;密钥沿用"设备密钥在库内",避开钥匙串弹窗问题(见 `KEYCHAIN_UX_PLAN.md`)。

任何一项不过,回到路线选择重新评估。

### 阶段 1:骨架与日用最小集(2–3 周)

- 同仓库开 `v2` 分支,1.x 代码留作规格参考。
- 主布局:标题栏快连、左侧栏、标签页、底部工作台、右侧活动栏。
- SSH 与本地终端、连接与目录管理、密钥与代理。
- 里程碑:自己能日常使用。

### 阶段 2:数据迁移与主要功能(3–4 周)

1.x 数据库迁移器、SFTP 与传输队列、远程编辑、监控、进程与网络面板、命令库、导入导出(含 FinalShell)。

### 阶段 3:差异化功能(4–6 周)

云同步 v3、Agent 网关、Shell 集成与 OSC。MCP 端点目前没有 token,在此补上。

### 阶段 4:发布(2–3 周)

三平台打包、签名与公证、自动更新(1.x 只有检查更新)、i18n、2.0 beta。1.x 进入只修 bug 的维护期,2.0 正式版后归档。

### 阶段 5:新功能

端口转发、跳板机、分屏等 1.x 没有或被回退过的功能。

## 风险

- **mygo 未到 1.0**:约 3 周 8 个版本,多次 breaking change,单人维护。对策见"原则"。
- **已知框架问题**(来自 mygo issue 列表):老 Intel 核显上 Metal 可能挂起(#1566)、CJK 排版(#1304)、更新器窗口问题(#1616)。Linux 托盘依赖 `libayatana-appindicator3`。
- **平台分发**:macOS 签名与 DMG 必须在 Mac 上构建;在非 Windows 机器上构建 Windows 安装包需要 NSIS。
