# 阶段 1 验收映射

本表的 PASS 指 Linux box 上的代码与自动化验证，不代表 Windows/macOS 或真人长时间日用验收。

| 计划项 | 实现 / 验证 |
|---|---|
| v2 分支、保存计划、固定 mygo | 本地 v2，自 origin/main 3c5e5a8；docs/NEXTSHELL-2.0-PLAN.md；v2/go.mod v0.8.3 |
| 原生主布局 | appui.View：集成标题栏快连、左侧目录、标签、工作台、右侧活动；TestNativeLayoutAndNavigation + Linux smoke |
| 标签交互 | 拖动/排序、重命名、关闭、复制、成功后原位重连、切换器与屏幕文本预览；TestTabDragReordering / TestTabsCommandsPreviewAndReconnect |
| 工作台 / 本地终端 | 真实 PTY、发送按钮与 Enter 执行、持久历史；TestNativeLocalTerminal / TestRealLocalWorkbenchAndHistory |
| SSH 认证 | in-process Go SSH server 实测 password/key/agent/keyboard-interactive、坏密码、加密私钥；TestSSHAuthentication / TestKeyMaterial |
| 指纹 / keepalive | 1.x SHA256/legacy SHA256/MD5/hex 规格移植；首次确认、持久 pin、拒绝变更；响应/不响应 keepalive 均测 |
| SOCKS4/4a/5 | 本地代理 + 真实 SSH 握手、用户名密码、拒绝响应、错误密码、握手取消 |
| 编码 | GBK/GB18030/Big5/Shift-JIS 真实 SSH 通道回环，并断言服务端收到的固定编码字节 |
| 连接与目录管理 | CRUD、复制凭据、树/搜索/折叠、table/grid、批量认证、回收站；目录引用/循环/并发循环/同级重名/深度保护 |
| 密钥与代理管理 | 密钥生成/文件与粘贴导入/口令/改名/删除，SOCKS 代理 CRUD；核心测试 + TestKeyAndProxyManagementForms |
| 存储与密钥 | 纯 Go SQLite，新 schema，设备密钥在 DB 且重启保持，密文/AAD 防替换，拒绝旧库与损坏 key；storage Go tests |
| Platform 隔离 | 核心依赖树无 mygo；UI 的系统操作经 Platform；TestDesktopSSHTrustCloneAndReconnect 验证提示、元数据不泄漏密码与重连 |
| 1.x 规格移植 | fingerprint/key-material 测试，Node scrypt/AES-GCM 确定性向量；阶段 2 的迁移器在 2.5 已取消；保留 FinalShell/导出文件测试 |
| OSC 133 | alex 批准延期；保留接口，不实现观察器，能力 false |

## 不应误读的边界

- 未实现/未验收跨平台 SSH agent 命名管道、托盘、多窗口、IME；这些没有被记为 PASS。
- 批量绑定以逐连接写入实现：失败时显示错误，前面已成功的连接不会回滚成全批次原子事务。
- 活动记录和命令历史均为本地功能；不是阶段 2 命令库，也不是阶段 3 Agent/MCP 面板。
- “自己能日常使用”目前只有 Linux 自动化日用路径证据；仍需 alex 真人实际服务器与 IME 验收，不能声称已经替代其 1.x 日常环境。
