# 阶段 2 实现及验收范围

2026-11-10 SGT（Asia/Singapore，UTC+8）。只在本 box、`v2` 分支提交；未 push/PR。阶段 3 未开始。

## 已实现与自动验证

| 范围 | 实现 / 证据 |
|---|---|
| 1.x DB | `migrate.Run` 只读 SQLite 快照；识别已知表及 <=29 schema，DB device_key 优先、fallback 经 Platform；本地连接/凭据/密钥/代理/空目录/命令/历史、隔离导入根、幂等 marker、事务回滚、新设备密钥重新加密 |
| 数据保护 | 真实 SQLite fixture：源文件前后字节相同、无 WAL/SHM；坏密钥/坏引用/损坏密文/未来 schema/目录环/非法端口/重复 ID 均不部分写入；目标既有数据和历史不被覆盖 |
| SFTP | 真实 Go SSH + SFTP 服务端：浏览、创建/重命名/删除、上传下载、禁止意外覆盖、POSIX 原子替换、权限、取消与暂存清理、拒绝链接、跨服务器流传输 |
| 队列 / 打包 | 并发 2、字节进度、取消/失败/重试；真实 SSH 执行 tar.gz 上下行、新目标约束、shell 引号注入测试；拒绝路径逃逸/链接/特殊文件/超限展开、损坏 gzip trailer |
| 远程编辑 | 真实 SFTP：文本上限/编码/二进制拒绝、模式保留、旧 hash 冲突；自建外部编辑器进程 + argv、运行中自动回传、成功清理、冲突恢复副本 0700/0600 |
| 监控 | 1.x 系统/进程/网络解析规格；CPU 差分/计数器重置/内存 fallback/IPv6/缺 PID；真实 SSH 执行 Linux probe，取消 exec 不破坏父 SSH；仅对测试自建 sleep 进程验证信号 |
| 命令库 | 存储 CRUD、分组、当前/全部会话；Platform 真实双 SSH 会话发送，UI 全部会话执行确认 |
| 交换协议 | `nextshell-connections` v1，`b64##` + salt32/iv12/tag16/ciphertext、scrypt 16384/8/1、AES-256-GCM AAD `nextshell-connection-export`；Node 固定向量逐字节比较、双向解密、篡改/错密码拒绝、明文省略密码 |
| FinalShell | 只用 NextShell 1.x TS 实现作规格，自写 Go Java Random / MD5 / DES-ECB；Node fixture 验证 unsigned 与 signed fallback；数值 authentication_type 不猜含义；目录预览/警告/导入/密钥待重绑真实集成 |
| 原生 UI / 边界 | 六个新增活动页、预览确认、编辑脏文本保护、进程确认、文件拖入 handler、OSC 7 跟随经 Platform；headless 渲染含填充数据；AST 测试禁止核心依赖 UI、禁止 appui 直接系统 I/O |

测试服务器只监听 loopback，使用测试自建临时目录及进程；没有连接或修改用户真实远端服务器。

## 规格来源与向量复现

仅参考本仓库 1.x：`packages/security/src`、`packages/storage/src`（含 connection-folders.test.ts）、`packages/core/src`、`import-export*.ts` / `.spec.ts`、`finalshell/decrypt-password.ts`、`monitor/*parser*.test.ts`。不是移植完全部 134 个测试文件。未参考任何第三方同类客户端。

```
cd v2
node internal/security/testdata/generate-vector.mjs
node internal/exchange/testdata/generate-vectors.mjs
go build ./...
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
```

向量逐字节验收指同一盐/IV/明文下的 crypto 布局和密文，并检查一份 TS 风格 JSON fixture 字节；不是声称每个合法 JSON 的序列化表示都必须唯一相同。正常导出随机 salt/IV。

## 明确限制 / 偏差

- DB 迁移保留的是**查询白名单表的加密 JSON 快照**，不是源库完整备份。云数据、回收站、偏好、模板参数只归档不激活；源库应保留。`Store.LegacyArchive` 可读，尚无 UI 恢复功能。旧路径型私钥需手动重绑。
- keytar `NextShell` / `device-key` 是明确 stub；只在缺少 DB key 且有旧密文时请求 fallback，失败即停止迁移。
- 导入冲突仅 skip / duplicate，没有 overwrite。XOR 读取按计划删除；导出只含公钥引用，不含私钥。
- 打包依赖远端 Linux tar + GNU mv，不覆盖/合并既有树；无断点续传。取消/断网后远端 `.part` 清理仅尽力而为；不承诺所有远端/文件系统均可原子 no-replace。
- 远程编辑仅 UTF-8、8 MiB；hash 检查不是服务器 CAS，存在检查与重命名间并发写入窗口。外部编辑器必须等待退出，失败保留明文恢复文件（私密目录）供用户自行取回及清理。
- 监控远端仅 Linux，网络为排除 lo 的接口聚合、无历史图表/接口选择；ss/netstat 缺失或权限不足会明确警告，不假造进程信息。
- 命令和历史为本地明文；模板仅保留元数据，不展开参数。
- 中文 IME、Windows/macOS 真机、硬件 GPU、托盘/多窗口、系统拖入及真实 GUI 外部编辑器 UX：**UNVERIFIED**。headless 和自建编辑器进程测试不等于这些验收。
- OSC 133：**deferred, waiting on upstream**；不增加自有流观察器、不改/fork mygo。

实际最终命令、测试计数、提交及日志位置见 PHASE2-REPORT.md。
