# NextShell 独立云协议 version 2 — 离线契约

2026-11-11 北京时间（Asia/Shanghai，UTC+8）。**这是离线草案/本地实现，不是经过独立审计的生产服务。** 代码 `v2/internal/cloudv2`、`storage/cloud.go`；无 HTTP、登录、后台上传、真实服务器。阶段 3 才联网。

## 不兼容范围与威胁边界

- 不兼容旧 cloud sync v3，也不与 v1 客户端同一工作区互通。不调用旧服务器、不转换旧工作区，不借用 v1 数据 ID/密钥/认证。
- 服务端拥有认证材料、wrapped DEK、密文和公开路由 ID，不能据**认证材料**推导 KEK/DEK。元数据、名称、地址、目录、时间、标签、命令、私钥、密码都在完整快照密文内。
- 不隐藏包长、访问时间、同工作区关联或提交链。弱加密口令仍可能被离线猜测；已持有 DEK 的客户端能解密/构造工作区快照。本机失陷、整个 DB 被盗不在本地设备密钥保护范围内。
- 首次设备无可信 head 时，无法仅根据不可信服务器证明它给的是“最新”快照；已保存可信 head 的设备仅接受其直接子提交。真实多设备一致性、历史抓取/信任锚、成员撤销/恢复/密钥轮换与审计是阶段 3 上线门槛，不能拿离线 fixture 代替。

## 1. 认证完全独立

`NewAuthToken`：独立 CSPRNG 32 字节，base64url 无 padding。服务端校验器：

```
SHA256(UTF8("nextshell/cloud/v2/authentication-verifier\0") || tokenBytes)
```

高熵 token 不是用户密码，服务端只须存 verifier。`VerifyAuth` 常量时间比较有效 token 的 verifier。token 不进入任何加密 KDF，用户加密口令不进入认证路径。token 的跨设备独立授权分发、TLS、有效期、撤销、速率限制尚未实现；`MemoryServer` 仅用一个 verifier 演示离线 CAS。

## 2. KEK、DEK 与用途分离

`WrappedKey` 外层精确字段：`version=2, workspaceId, keyId, kdf, nonce, ciphertext`。ID 是不含业务名称的 UUID；`kdf={name:"scrypt",n:32768,r:8,p:1,salt:<base64随机32字节>}`。只接受这些常数，先校验再派生，避免恶意参数降级/资源耗尽。

加密口令：原样 UTF-8，不做 Unicode 归一化，非空且 ≤4096 字节。KEK=32 字节 scrypt 输出：

```
scrypt(UTF8(passphrase), UTF8(JSON.stringify([
 "nextshell/cloud/v2/kek", 2, workspaceId, keyId, Base64(salt)
])), N=32768,r=8,p=1,len=32)
```

随机独立 DEK32 由 KEK 使用 AES-256-GCM 包裹；nonce 随机 12 字节，128-bit tag **拼在 ciphertext 末尾**。AAD 是紧凑 UTF-8 JSON 数组：

```
["nextshell/cloud/v2/wrap-dek",2,workspaceId,keyId,kdf]
```

`kdf` 对象序列化顺序固定 `name,n,r,p,salt`。改变认证 token 不改变 DEK；改变加密口令用新 salt/nonce 重包裹**同一** DEK（`WrapKey`）。这不是 DEK 真轮换或撤销已有 DEK 持有者；本地 SaveWorkspace 拒绝隐式替换已有 DEK/keyId。

## 3. 完整密文 Snapshot 与提交

外层 `Commit` 精确字段：`version,workspaceId,keyId,commitId,parentId,nonce,ciphertext`；v2 只接受 version=2。独立随机 commit UUID；首提交 parent 为空，以后是期望 head。AES-256-GCM/DEK、nonce12、tag16 尾置，AAD：

```
["nextshell/cloud/v2/snapshot",2,workspaceId,keyId,commitId,parentId]
```

`Snapshot={version:2,workspaceId,name,records}` 完整加密。每个 record 含 `kind,id,meta,tombstone?,data?,secret?`：

- kind 仅 folder/connection/key/proxy/command；资源身份为 `(kind, UUID)`。
- meta：scope（workspace UUID）、createdAt/updatedAt（UTC 毫秒 ISO）、revision≥1、可选 deletedAt。data 中重复的 ID/meta 必须一致。
- connection/proxy 的 secret 是密码字符串；key 的 secret 是包含 Private/Passphrase 的 JSON 字符串。它们只出现在 Snapshot 明文**内部**，wire 外层没有明文秘密。非秘密记录不允许 secret。
- folder sortIndex、key metadata、command appendCr、keepalive 未指定态与本地信任字段在 data 中。连接回收站状态与 deletedAt 一致；永久删除 record 是 tombstone，仅保留 kind/id/meta，无 data/secret。
- 所有目录/key/proxy 引用必须在同快照同 scope；校验重复、缺引用、目录环/深度、兄弟同名、未知字段/版本、秘密/类型、时间与大小。≤100000 条、单 data/secret ≤4MiB、wire JSON ≤64MiB（明文还预留 base64/头部开销）。Go 包 `EncodeCommit/DecodeCommit/DecodeWrappedKey` 是严格线格式边界，不应换成宽松 JSON.Unmarshal。

v1 连接文件仍是完全独立的 `nextshell-connections` version 1：scrypt16384 与 `salt||iv||tag||ciphertext`，AAD 不同。不能共用新云盐、密钥、AAD 或布局。

## 4. CAS、本地 DB 与原子性

`MemoryServer.Accept` 只按独立 token 校验认证、外层字段与 parent=head：accepted 或 diverged，资源与命令共用一个 head，不存在单独命令版本。它不解密业务数据，读写均克隆缓冲区；它不是可部署服务器。

schema 3 是自己的 v2 profile 升级：application_id=0x4e534832；resources 追加 scope/created_at/updated_at/deleted_at/revision 列；profile_state 全局 generation 防过期导入，scope_state 防跨工作区互相误判 dirty；deletions 保留永久删除；workspaces 与 sync_state 保存本地名称、可信 head、同步 baseline。资源/命令稳定 UUID，no-op save 不改内容版本；删除 UUID 不隐式复用，暂不 GC。

工作区 auth token、wrapped DEK、DEK 用本地随机 device key 加密保存在 secrets，以 workspace 专用 AAD 绑定；列表只返回 workspace ID/名称。元数据/命令在本地 DB 并未整体加密，**上云前才整包加密**。device key 在同一 v2 DB，不读 v1 DB/keychain。

`SyncSnapshot` 用 SQLite 一致读事务生成全快照；`AcceptPublished` 验证 head 和抓取时 generation 后才更新 baseline；`ApplySyncCommit` 先 AEAD 认证解密/验证，再在事务内复核 head/baseline/generation、scope/版本/引用/已知记录和 tombstone，写入并推进 head。入站保留来源时间/revision，不替换成 now。已知资源不能无 tombstone 消失，内容不能在相同 revision 偷换；本地有未同步编辑就停止，不默默覆盖。真正服务器冲突 UI/重试/崩溃后远端已成功但本地未记 head 的协调流程在阶段 3。

## 5. 验证证据与上线前未验证

- Node 独立 scrypt/AES-GCM 已知答案：`cloudv2/testdata/node-vector.json`；Go 双向逐字节校验 KEK、wrapped DEK 和 Snapshot 密文。
- Go 测试覆盖认证/加密独立、重包裹、不同盐/nonce、AAD/版本/参数篡改、严格 JSON、回放/CAS、两份本地 DB 全资源秘密往返、tombstone、dirty 拒绝和事务回滚；不是形式化安全证明。
- **UNVERIFIED**：独立密码学审计、真实网络/服务器、真实设备间认证与恢复、TLS/撤销/流控、可信新设备引导、恶意服务器分叉检测、真实 DEK 轮换/成员管理。正式上线必须复审契约和威胁模型，不能直接把 MemoryServer 当生产服务器。
