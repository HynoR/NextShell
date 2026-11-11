# 阶段 2.5 实施报告

2026-11-11 09:30 北京时间（Asia/Shanghai，UTC+8）。已完成本地实施并停止在阶段 2.5；没有开始阶段 3 联网。

## 提交与范围

- **`9c38a98`** `feat(v2): replace legacy migration with explicit exchange and independent offline cloud protocol`：下面全部实现、回归测试、协议向量与活跃文档。
- 本报告和计划措辞澄清作为随后独立 docs 提交；其 hash 见本报告提交后的 `git log -1` / 交接回复（避免自引用 hash）。
- 仅 `/workspace/NextShell` 的 **v2** 本地提交，无 push/PR/云代理/外部写入。已有 `stash@{0}`（`pre-existing-uncommitted-before-v2`）原样保留，未读取/丢弃/提交其中改动。未再次访问 finallyshell 或其他第三方 SSH 客户端代码。

## 已完成

1. **删除旧架构兼容**：移除 `internal/migrate`、旧设备密钥/keychain stub、Platform 方法、迁移 UI、旧迁移测试、archive/marker/history-merge 逻辑。保留通用原子文件导入和正常历史 API。无 v1 DB 自动升级或读取路径；外来 SQLite profile 仍拒绝。
2. **自主 schema 3**：仅升级自己的 v2 schema 2，保留已有 UUID/device key/秘密；scope、UTC 毫秒时间、revision、删除日志、workspace/vault、generation/baseline/head。连接/目录/key/proxy 不跨 scope 引用；no-op 不增内容 revision；永久删除 UUID 不隐式重用。保留目录排序、key 指纹/类型/bits/comment、命令 appendCr、keepalive 未指定/关闭/开启语义。缺 key 而已有 secrets 时 fail-closed。
3. **v1 文件交换**：加密/明文 `nextshell-connections` version 1 导入/导出，明确拒绝 XOR；明文输出省略密码。默认 skip；rename-copy 新 UUID/唯一显示名；显式确认 overwrite 唯一本地端点，保留目标 ID/createdAt、缺省密码/代理/信任与其他未提供字段；明确空密码才清空。预览列目标 ID，generation 改动使预览过期，多候选拒绝盲覆写。只能导入 local scope。给定 key 指纹不匹配时不降级同名匹配，保留待重绑来源引用。
4. **真实规格验证**：直接调用第一方 v1 TS 加解密/解析函数生成向量并验证 Go→TS / TS→Go；固定 salt/iv 时完整文件 envelope/JSON 逐字节一致。TS bridge 仅将未调用的 FinalShell crypto-js 依赖设为 throwing stub。保留原 FinalShell 解密、scrypt/AES-GCM 与真实 loopback SSH/SFTP/传输/监控测试。未运行完整 v1 GUI，不能据此声称全部客户端 UI 验收。
5. **新云离线实现**：独立 `cloudv2` DTO/crypto/严格 wire codec、opaque MemoryServer CAS、加密本地 workspace vault；一致 SQLite 快照、两份本地 DB 完整资源/秘密/tombstone 往返、dirty/head/revision/跨 scope 保护和原子失败回滚。Node 独立已知答案逐字节校验新协议 KEK/wrapped DEK/Snapshot 密文。核心架构测试继续禁止 UI 依赖。

## 新协议要点

- **version=2，自有协议；不兼容旧 cloud v3，不与 v1 同工作区互通。** 无 HTTP/login/后台上传，真实服务器接入留到阶段 3。
- 独立随机 auth token32 + 用途分离 SHA-256 verifier；独立加密口令经 scrypt **32768/8/1**、随机 salt32 与 workspace/key/purpose 域派生 KEK；随机 DEK32 加密整快照。认证材料不进入加密 KDF。
- AES-256-GCM、随机 nonce12、tag16 尾置；AAD 绑定版本/用途/workspace/key/commit/parent。**元数据、命令与所有秘密一起加密**，外层只有 opaque IDs、KDF/nonce/密文；仍泄漏长度、时间与关联。
- 改 auth 不改 DEK；改加密口令只重包裹相同 DEK。整工作区含命令共用 CAS head；可信本地 head 拒绝回放，本地 dirty 不默默覆盖。新设备无可信锚时无法自行证明服务器给的是最新历史。
- 本地 device key 在同一 DB，workspace auth/DEK 用其加密；本地资源元数据/命令不是全库加密。整 DB 被盗仍不在此保护边界。

## 实测结果（最终无缓存，代码 `9c38a98`）

Go **1.27.3 linux/amd64**：

| 命令 | exit | top-level 通过 | 通过 runs（含父/subtest） | leaf 通过 | 失败 | 跳过 |
|---|---:|---:|---:|---:|---:|---:|
| `go build ./...` | 0 | — | — | — | — | — |
| `go vet ./...` | 0 | — | — | — | — | — |
| `go test -json ./... -count=1 -timeout=180s` | 0 | **113** | **245** | **221** | **0** | **0** |
| `go test -race -json ./... -count=1 -timeout=240s` | 0 | **113** | **245** | **221** | **0** | **0** |

两轮均 **12 个含测试包、245 run 事件**，package failure=0；另有 2 个 cmd 和 testutil 无独立测试，不计作测试 skip。计数来自 Go JSON `pass/Test` 事件；leaf 排除存在子测试的父节点，不是断言数。两份 stderr 均 0 bytes；未发现 race 报告。

- `go run ./cmd/nextshell --db :memory: --smoke`：DISPLAY=:17，原生单窗/本地终端自动退出 **0**；明确无 GPU，使用软件渲染；出现 AT-SPI 辅助总线警告，未影响此次 smoke。
- `go run ./cmd/probe terminal`：exit **0**，`dir="/tmp"`、screen text 含 `probe-ok`；不把它当 OSC 133 或人工 GUI 验收。
- 原始日志/JSON/计数：`/workspace/NextShell-notes/phase2.5-{build-vet.log,go-test.json,go-race.json,test-summary.json,native-smoke.log,terminal-probe.log}`。

## UNVERIFIED / 已知边界

- **未联网、未部署新服务器、未做独立密码学审计**；实际登录/token 生命周期/TLS/恢复/新设备信任锚/恶意服务器分叉检测/DEK 真轮换/成员撤销均留阶段 3。MemoryServer 不是生产服务。弱口令仍受离线猜测，不能把换口令当成员撤销。
- Windows/macOS 实机、中文 IME、硬件 GPU、托盘、多窗口渲染、真实桌面文件拖入/外部编辑器 UX 继续 **UNVERIFIED**。本次只验证 Linux native smoke、headless API/原生 UI Tester 与本地协议服务器。
- **OSC 133：deferred, waiting on upstream**，无 observer/fork，能力 false。
- v1 连接文件不是完整备份：原 IDs/时间、私钥/代理/信任/命令/工作区/空目录等不在文件内；Shift-JIS 兼容导出拒绝，不伪装无损。未测试完整 v1 GUI 导入流程。
- 继承阶段 2 的限制：远程编辑是 hash 乐观检查，不是服务器 CAS；打包依赖 GNU tar/mv；断连暂存清理为 best effort。没有在本阶段扩大这些保证。

## 偏离与小取舍

- 无未获批准的架构偏离；按 alex 最新决定取消旧云互通，故不保留 v1 cloud v3 schema/认证兼容。只做离线层，未开始阶段 3。
- v2 自有 schema 2→3 内部升级不是 v1 迁移。已有历史 opaque settings 不自动清理/恢复，避免删除用户数据。
- 默认按 local `(host,port,username)` 精确找冲突，不做 DNS 合并；rename-copy 名称在本地连接名集合内避重；多候选 overwrite 直接要求换策略，不自动选一条。
- 应用 keepalive 默认启用/30 秒；原 v2 显式关闭保留，wire 未指定态在连接边界解析。命令 appendCr 缺省 true。
- 新云使用固定 AES-256-GCM、tag 尾置；加密口令非空 UTF-8、≤4096 bytes，不做归一化；wire ≤64MiB、≤100000 records、单 data/secret ≤4MiB。强度/恢复政策须在阶段 3 上线前复审。
- 文件仍导出 v1 格式，没有假装完整 v2 备份；XOR/矛盾的 password omission/null 明确拒绝。无需 alex 再决策的阻塞项。

计划：`docs/v2/PHASE2.5-PLAN.md`（外部同副本 `phase2.5-plan.md`）；精确协议：`docs/v2/CLOUD-V2-OFFLINE.md`。
