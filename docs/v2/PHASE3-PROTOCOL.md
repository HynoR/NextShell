# 阶段 3：本地参考联机契约与安全边界

本阶段只接本机参考 HTTP 服务/httptest，不接任何真实外部服务。不与 v1 同工作区互通。协议加密格式仍见 PHASE2.5-PLAN，现为其添加传输与客户端调度。

## Cloud HTTP

- URL 必须是 `http(s)://<literal loopback IP>:<port>`，例如 `http://127.0.0.1:41888`；不接受域名、账户信息、路径前缀或重定向。关闭环境/系统代理。当前没有服务在默认 URL 自动启动；参考 handler 位于 `internal/cloudhttp/reference.go`，集成测试用 `httptest.NewServer`，不是可部署的生产服务器。
- `POST /v2/login`：`Authorization: Bearer <独立随机 bootstrap token>`，返回 `{version:2,accessToken,expiresAt}`。后续请求用短期随机 access token；401 自动登录重试一次。不会把加密口令用于登录或上传口令。UI 拒绝直接复用认证 token 为加密口令。
- `GET /v2/workspaces/<uuid>?after=<trusted head>`：返回 `{version:2,head,commits:[...]}`，按父链顺序排列、最多 256 条。客户端检查工作区、每个 parent 及最终 head，随后逐个认证解密。未知/分叉 head 返回 409，不强行重置本地锚点。
- `GET /v2/workspaces/<uuid>/key`：返回 version 2 WrappedKey；独立加密口令在客户端 scrypt 派生 KEK 后解开随机 256-bit DEK。
- `POST /v2/workspaces/<uuid>`：`{version:2,commit,wrapped}`，服务端按 commit.parent 与当前 head 做 CAS，拒绝重放/旧 head。客户端还校验 ACK 的 version / head，才标记已发布。
- WrappedKey KDF 为 scrypt N=32768/r=8/p=1，32-byte 随机 salt，包含 `nextshell/cloud/v2/kek` 用途/版本/工作区/密钥 ID 的域分离；认证 verifier 使用完全不同的用途和随机 token，绝不进入 KEK KDF。AES-256-GCM 封装 DEK / snapshot，AAD 绑定身份与父链。
- 工作区名称、所有资源元数据、命令、连接秘密均在加密 snapshot 内；服务器仍能观察路由 UUID、key/commit/head ID、时间/长度/流量等，不宣称隐藏全部交通元数据。弱口令仍可能遭离线猜测；新加入设备没有外部见证时无法证明服务器没有隐藏最新历史。

## 同步与冲突

首次“创建”只建本地工作区，需点击立即同步发布后另一设备才能“加入”。加入需 UUID、认证 token、独立加密口令。已保存工作区可用本地设备密钥解密持久化凭据重新登录，不自动联网。

每工作区独立 generation/head/baseline；本地普通资源不会悄悄进入云，必须明确复制选中的本地连接。复制创建新 UUID，复制依赖目录/密钥/代理及其秘密；同级目录重名加数字后缀。删除经 tombstone 同步。

客户端每次同步串行执行；远端有新提交且本地脏时进入 conflict 并停止后台，不静默覆盖。可暂停保留本地，或明确确认采用远端：验证完整锚定链，然后在同一事务中设备密钥加密备份原工作区并替换，recovery ID 在状态 UI 展示。恢复快照读取 API 已有，尚无恢复浏览/恢复 UI。没有字段级自动合并、keep-local rebase、分叉服务端历史强制覆盖。

后台默认**关闭且不跨重启自动恢复**；用户开启后立即同步，正常间隔 30 秒，失败指数退避最多 5 分钟，冲突暂停；关闭/退出取消并等待工作完成。端点地址仅本次运行配置。当前是全量加密 snapshot，不是增量/分页/压缩优化协议。

参考服务：单账户、内存保存、最多 128 个 15 分钟会话、256 条总提交，发布包/总存储字节均受 64 MiB 限制，拒绝非回环 Host/客户端及浏览器 Origin。没有账号注册、成员权限、多租户、持久化、找回/撤销/DEK 轮换、生产 TLS、限流运维或灾备；不得用于生产。认证 token 轮换不等于成员撤销，重新 wrap 同一 DEK 不等于 DEK 轮换。

## Agent / MCP

手动启用后监听 `127.0.0.1:41777/mcp`，默认禁用/暂停；token 为独立 32-byte 随机 base64url，设备密钥加密存储，不与 cloud token 共用。每个 POST/GET/DELETE 都做 bearer 校验，拒绝浏览器 Origin、非回环 RemoteAddr/Host、query token。正文 64 KiB、并发请求 32、会话 32、闲置 10 分钟。

协议由官方 MCP Go SDK v1.8.0 负责，声明支持 2025-03-26 / 2025-06-18 / 2025-11-25 的会话型 Streamable HTTP（POST JSON + GET SSE）。未支持 2026-07-28 无会话审批 profile。官方 SDK 客户端、旧 2025-06-18 初始化与真正 GET SSE 有集成测试。旧 v1 npm bridge 不带 token 时不能直接调用；需调用端增加 Authorization，不宣称旧二进制零配置兼容。

10 个工具保留同名用途：host_list、session_open/list/history/read、command_search/save、exec、session_send_signal/focus。查询/读屏/命令查询返回脱敏数据，不暴露凭据接口；变更都先在应用内批准，绑定原 MCP 会话和精确原参数，2 分钟期限、拒绝不可借轮询覆盖、一次消费。紧急停止取消正在执行的 ctx；token 轮换撤销会话并暂停，须手动恢复。

执行只借用**已连接远端 SSH** 的独立 exec channel，继承经过安全 shell quoting 的 OSC7 cwd，超时默认 30 秒/最多 120 秒，stdout/stderr 各 512 KiB；不偷偷往交互 shell 输入。危险预设 + 本次运行黑名单、`.env` 显式许可 + 每次审批均生效。黑名单不是完整 shell parser/sandbox，脱敏也不是通用 DLP，持有 token 的本机程序仍应可信。

与 1.x 的差异：不提供 Auto 无审批、本地/前台执行、用户 pending-input/OSC133 完成等待与命令块历史；recent-host 排序/部分返回字段没有完全复刻，当前 host_list 以存储顺序最多 20 项。session_read 当前只有 screen 标签，但实际通过官方 Text() 读取屏幕与 scrollback 的尾部，不是精确 viewport 分界；history 返回 integrationAvailable=false / 空 entries，不伪造命令历史。焦点仅应用内选标签，不保证从其它应用前台唤醒操作系统窗口。

## Shell / OSC

全部通过 mygo v0.8.3 官方 terminal 插件的 Dir/Text/Title/OnTitle/OnBell/OnNotify；OSC 8 超链接保留插件默认交互，没有自研 VT。OSC9/777 通知当前转应用内消息，未接 OS 通知服务；OSC52/5522 的远程剪贴板读写继续默认拒绝。

bash/zsh 目录钩子是本次会话脚本：百分号编码路径字节，保留原 prompt hook、幂等，不编辑 rc、不下载脚本、不自动注入；只把命令放入工作台供人工审阅执行。Bash 已实际执行测试；zsh 未安装，测试显式 skip / UNVERIFIED。

OSC133：**deferred, waiting on upstream**。不启用自研观察器/截流/第三方 SSH 客户端代码，保留已有空接口 seam。中文 IME、macOS/Windows、实际托盘/多窗口、GPU/系统通知等不由 Linux 软件渲染 smoke 证明通过。
