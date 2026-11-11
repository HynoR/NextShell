# Phase 2.5 最终执行计划

2026-11-11，北京时间（Asia/Shanghai，UTC+8）。alex 已决定不接受 v1 云安全边界；本版替代 11 月 10 日的 cloud v3 兼容草案。下面是最终计划；实施证据另见 PHASE2.5-REPORT.md，不将计划当作验收。

## 1. 边界

- 不读、不迁移 v1 nextshell.db/device key/keychain；只有用户 v1 导出连接文件→v2 手工预览导入。
- 文件继续支持 nextshell-connections version 1 加密/明文，拒绝 XOR；继续导出 v1 连接交换格式。新应用不等于新文件版本。
- 云使用独立、显式 version=2 的 NextShell 自有协议，**不与 v1 客户端同工作区互通，不兼容旧 cloud sync v3**。不自动接入、转换或覆盖旧云工作区。
- 只实现离线协议/DTO/本地存储/加密层与 fake-server 测试；不添加真实 HTTP 客户端、账户登录或后台上传，联网在阶段 3。
- 保留 v2 自己的数据，schema=3 内部升级只识别 v2 application_id；不自动清空历史归档或 profile。领域模型和 DB 不照抄 v1。

## 2. 删除与保留清单

### 删除/改写

1. 删除 `v2/internal/migrate/` 整包及旧 SQLite schema/旧设备密钥/fallback/旧业务数据归档测试。
2. 移除 `appui.Platform.MigrateLegacy/ReadLegacyDeviceKey`、desktop 对应实现/依赖、UI MigrationPath/ConfirmMigration、旧 DB 路径和按钮；移除 desktop 的 stub 断言，架构测试包清单去掉 migrate。
3. storage 删除未使用 `LegacyKeyReader`，删除 Bundle.Archive、LegacyArchive、迁移 marker/Imported/ErrAlreadyImported；Bundle.History 合并也只有迁移使用，可移除该分支，**保留正常命令历史 API**。
4. 将 ApplyBundle 收敛为事务型文件导入写入；保留目录/引用/冲突校验、凭据加密和全有全无回滚测试，不要因为删迁移就删掉共同使用的导入能力。
5. 活跃 README、PHASE0、验收清单及长期计划补上新边界；既有 PHASE1/2 报告保留历史，标注“迁移能力已在 2.5 取消”，不伪造旧验收。实现时另写 PHASE2.5 报告。实施时同步修订这些仓库文档。
6. 既有 v2 DB 若已有 `legacy-archive:*` / `import:*`，不自动读取转换、不自动删除；不恢复其 v1 数据。历史数据清理由用户以后明确发起，不写清库逻辑。

### 必须保留

- fresh v2 DB、设备密钥、凭据加密、外部 DB 拒绝保护（只识别自己的 profile，不查询 v1 业务数据）。
- exchange 文件 DTO/编解码、指纹匹配、目录物化、预览与原子导入、加密导出、FinalShell 文件导入/解密（它不是 v1 DB 迁移）。
- security 的 scrypt/AES-GCM 及 Node 向量：它们也是文件/新云协议，不是“旧架构包袱”。本地 device-key crypto 独立保留。
- SSH/SFTP/编辑/监控/命令库、Platform 边界与真实本地服务器测试不变。OSC 133 继续等待上游。

## 3. 文件兼容范围（建议默认，不需要另选架构）

| 输入 | 支持策略 |
|---|---|
| `format:"nextshell-connections", version:1` + `b64##` | 必须支持，提供导出口令，完整解密后验证；BOM/前后空白兼容 |
| 同格式明文，`passwordsOmitted:true` 或未含 password | 必须支持；清楚显示“密码未随文件导出，需重新输入” |
| 同格式历史明文，password 是实际明文字符串、非 XOR | 支持，预览警告含明文凭据；按用户确认存入新设备密钥 vault |
| `passwordsObfuscated:true`（历史 XOR） | **继续明确拒绝，不误当明文密码**；提示在 v1 重新导出为加密文件。原长期计划也已丢弃此路径；兼容承诺要写清此例外 |
| 未知 format/version、坏密文/标签、错误口令 | 明确失败，目标 DB 不部分写入；不能猜成另一种格式或降级解密 |
| FinalShell JSON/目录 | 保留现有独立格式入口，不扩张到读取任何安装 DB |

**事实：**1.x 文件加密不是云 JSON envelope。文件是 `b64## + Base64(salt32 || iv12 || tag16 || ciphertext)`，scrypt N=16384/r=8/p=1、32-byte key、AES-256-GCM、AAD=`nextshell-connection-export`；云 secret 才是 `{v,alg,kdf,salt,iv,aad?,ciphertext,tag}` JSON。不能混淆 version 1 文件、version 1 envelope 和 cloud v3。

### 字段进入 v2 的映射

| v1 文件字段 | v2 处理 |
|---|---|
| name/host/port/username | Name/Host/Port/User；新建 UUID，验证端口及非空字段 |
| authType | password/agent 原义；privateKey→key；interactive→keyboard；只在适配器映射 |
| password | 缺省、空串、非空三态；新对象缺省表示未配置；已有对象覆盖时缺省不清密钥。明文 password 与 passwordsOmitted 自相矛盾的文件应拒绝/明确处理，不静默猜测 |
| sshKeyRef{name,fingerprint} | 在目标本地 scope 匹配；建议“有指纹必须相符，仅无指纹时允许唯一名称匹配”，否则保留来源引用/原 auth、标记待重绑，不默用同名错误密钥（比当前 fallback 更安全） |
| groupPath | 只作导入投影；剥掉 /server、/import、/workspace/<slug> 的线格式前缀，物化/复用 v2 实体目录；目录扫描相对路径应有 literal 标识，不能剥掉用户真正叫 server 的目录 |
| tags/notes/favorite/terminalEncoding/backspaceMode/deleteMode/monitorSession | 保留原义及字段 presence；模型可自由命名，边界输出恢复线格式字段名 |
| keepAliveEnabled/keepAliveIntervalSec | 改为可表示“未指定/显式关闭/显式开启+间隔”，不要只压成一个整数；未指定可能需要继承全局 SSH 设置 |
| exportedAt | 文件来源信息，不冒充每条记录的 createdAt/updatedAt |

**不可恢复/不能承诺无损：**原 v1 DB ID、创建修改时间、工作区归属/云版本、host fingerprint/strict 检查设置、代理配置、空目录/目录排序、私钥和 passphrase、命令库/历史/设置/回收站都不在这个文件契约内。sshKeyRef 不是私钥备份。导入只创建本地连接，不根据 groupPath 自动加入云工作区。即使同一文件往返，资源 ID 也不会恢复。

### 冲突与输出（建议）

- 只在**本地目标 scope**内按 `(host,port,username)` 找冲突，不拿同地址云连接做覆盖候选；不做 DNS/IP 合并。默认 skip，重复导入不产生新目录。
- `rename-copy`＝创建新 UUID，并生成不冲突显示名（如“名称（导入 2）”），不是只给原记录改名；允许同端点多配置。
- 增加显式 overwrite：预览精确列出目标，保留目标 UUID/createdAt；只覆盖文件明确携带字段，缺失密码/代理/信任策略不清空，空串密码需明确确认；更新 updatedAt。多个同端点候选不能任意取一个，要求选目标或改用 rename-copy。与事务校验一起提交，防预览后目标被改动。
- 连接交换默认**继续导出 v1 格式**，用户在 v1 也能按已读 parser 导入（推断，需双向实测）。默认加密；明文不带密码；不含私钥。无需为了 Go 重写发明 version 2 文件。
- v2 独有字段如 shift_jis、云关联和新设置不能假装 v1 可用；选中不可表示的连接时提示/拒绝该兼容导出，不静默改编码。完整 v2 备份格式以后另立独立 format，不把 version 改成 2 就宣称 v1 能读。


## 4. 自主 schema 与同步语义

保留 JSON resource payload，新增资源 scope/createdAt/updatedAt/revision/deletedAt、工作区表、同步状态与本地删除日志（上云时随快照整体加密）；字段/索引不受 v1 DB 约束。新资源 UUID 稳定，不每次拉取重建。工作区数据隔离，连接/key/proxy/folder 不跨 scope 引用；入站快照原子应用和 head 更新，失败全部回滚。local edits 与 remote apply 两种写路径，入站时间不改为 now；仅重加密不制造内容变更。

补 folder.sortIndex、key metadata、command.appendCr、keepalive 未指定/关闭/开启三态；保留本地信任策略。完整快照含所有活动资源与 tombstone，元数据、引用、命令、密码/私钥都在密文内。使用整工作区原子 CAS head（资源与命令一起，不再沿用 v1 分离命令版本）。本地 baseline/generation 检查防止拉取覆盖未同步编辑，双端变更停止，明确选择后才替换。

## 5. 新云协议 version 2（合理默认）

### 独立认证与加密

- 认证是独立随机 256-bit token，服务端存用途域分离的 SHA-256 verifier（高熵随机 token，不是可字典猜测的用户密码）；离线生成/验证，联网注册、TLS、撤销/速率限制交给阶段 3。
- 另用客户端输入的加密口令派生 KEK：scrypt N=32768/r=8/p=1，随机 salt32，并将 protocol/workspace/keyID/purpose 放入 KDF salt 域；口令原样 UTF-8，不发送服务端，不从 auth token 派生。
- 随机 DEK32 加密整个快照；KEK 只包裹 DEK。AES-256-GCM、随机 nonce12、tag16，AAD 绑定 version/workspace/keyID/commit/parent/purpose。认证 token、KEK、DEK 不复用。不同设备通过独立授权取得认证 token，并通过加密口令+wrapped DEK 解锁，不需要旧设备 key。
- 更换认证 token 不改变 DEK；更换加密口令重包裹同一 DEK，既有密文仍可读。DEK 真轮换/成员撤销后的历史访问控制另列阶段 3 方案，不假装换口令就撤销已有 DEK 持有者。

### 密文与版本

外层只允许协议版本、随机不含名称的 workspace/key/commit ID、opaque parent head、wrapped key 所需 KDF 参数、nonce/ciphertext；名称/地址/目录/凭据/标签/时间/命令均在整体密文内。包长/访问时间/工作区关联仍可见，不宣称隐藏流量元数据。

资源/命令统一 encrypted Snapshot；wire JSON 强校验版本、大小、KDF 常数、ID/引用/重复项。拒绝 version 1 与未知版本，绝不降级。CAS 推送期望 parent，服务端只存密文和 opaque head；离线状态机验证 accepted/diverged。客户端本地保存可信 head 后可拒绝回放/回滚；全新设备没有可信锚点时不能仅凭服务器提供的历史证明其最新，阶段 3 要解决信任引导。

这是自研协议草案与实现，**不是已通过独立密码学审计的生产协议**。不对恶意客户端/被控本机/弱加密口令离线猜测提供不切实际保证。正式联网之前需要审计、恢复流程、认证生命周期和真实服务器一致性验收。

## 6. 本地 secrets

fresh v2 随机 device key32 保存在 v2 DB。缺 key 但已有 secrets 必须报损坏，不能重生成；不查 keychain。文件密码/云加密口令只用于边界解密，转存使用本地 device key。工作区 auth token/DEK 在本地设备加密 vault 中，不能出现在列举元数据、日志、预览；本地数据库被整体窃取仍不在此保护范围内。

## 7. 顺序与验收

1. 删迁移专用包/stub/UI/tests/archive branches，保留通用事务导入、历史 API/外部 profile 拒绝。
2. v2-only schema 升级/元数据/scope/删除日志/工作区与 CAS 状态，保留旧 v2 数据和原子回滚。
3. 文件 adapter：默认 skip、rename-copy 新 UUID/名称、显式 overwrite 保留 ID/未提供凭据/路由；预览列目标及 generation，预览后变化报错；文件只入 local scope。
4. 新云 v2 crypto/全密文 snapshot/offline CAS/本地 vault，独立认证，无 HTTP 实现；冻结 JSON 契约及测试向量。
5. 真实 TS v1 函数生成的文件双向 fixture，坏密码/tag/XOR/版本/字段 presence、隔离与覆盖测试；crypto nonce/域分离/不同工作区/AAD 篡改/版本降级/回放/CAS/错误解密/原子 apply 测试；不以 auth verifier 代替加密测试。
6. go build/vet/test/-race 最终无缓存重跑，记录 top-level/pass runs/leaf/fail/skip；Linux native smoke/headless 回归；本地分批提交和中文报告。

粗估仍约 7–11 开发日（设计估算，非执行时长承诺）；不含阶段 3 服务端接入。小取舍按上面默认执行，无需再问 alex。真实云服务器、多设备真实共存、独立安全审计、IME/Win/macOS/GPU/托盘多窗口/GUI 拖入等继续 UNVERIFIED；OSC133 等待上游不变。

## 小取舍补充

应用 keepalive 默认启用/30 秒；连接的启用状态和间隔分别保留未指定态，显式关闭可保留 0 秒。新协议加密口令原样 UTF-8、非空且最多 4096 字节；不声称弱口令能防离线猜测，阶段 3 上线前必须确定强度/恢复政策。云协议使用固定 AES-256-GCM（tag 拼在 ciphertext 末尾），不是 v1 导出文件的 tag 前置布局。保留已删 UUID 的 tombstone，不自动复用或 GC。
