# 阶段 3 报告（含 Go 生态插入任务）

日期：2026-11-11（Asia/Shanghai，UTC+8）。仅 `/workspace/NextShell` 的 `v2` 本地工作/提交；没有 push、PR、cloud agent，没有连接外部真实云服务，没有读取 finallyshell 或同类 SSH 客户端源码。

## 本地提交

- `da5b5bd`：云 HTTP/参考服务/同步协调、Agent 核心、认证端点初稿、OSC7 脚本与 Platform 接口。
- `3f79a32`：**独立生态替换提交**，Goose、官方 MCP Go SDK、mygo editor / WatchFiles、依赖版本。
- `3acf768`：**独立生态补充提交**，隔离 Goose 全局注册表、校准审计说明。
- `3cc037e`：阶段 3 原生 UI、集成/回归测试、调度/ACK/审批/复制安全修正、协议说明。
- 本报告另作 docs 提交；最终提交号见 `git log` / 最终回报。

预先存在的未知改动仍保存在 `stash@{0}: On main: pre-existing-uncommitted-before-v2`，未混入 v2，也未清理用户数据。没有重新加入 v1 DB、旧 device-key / keychain 迁移或迁移 UI。

## 完成内容

1. **本地参考联机云客户端**：独立 version=2 协议，登录/401 重登、推送/拉取、父链/CAS/ACK 校验；后台同步、退避/停止/退出取消；原生工作区创建/加入/登录、状态、手动同步、后台控制和冲突 UI。显式选中本地连接后独立 UUID 复制到云工作区，复制依赖和秘密；删除 tombstone 同步。
2. **安全边界维持**：认证随机 token 不进入 KEK 派生；独立口令+scrypt 域分离派生 KEK，包装随机 AES-256-GCM DEK。名称/元数据/命令/秘密在加密 snapshot 内；不兼容 v1 工作区。UI 拒绝直接复用认证 token 为加密口令。URL 强制字面回环 IP、不代理、不跟随重定向；本轮没有公网连接。
3. **冲突**：远端领先且本地脏则暂停，不自动覆盖；可保留本地暂停，或明确确认采用远端。后者先做设备密钥加密恢复快照，再事务替换工作区；CAS 防止确认期间本地变化，其他 scope 不被替换。此恢复快照不是 v1 迁移存档。
4. **Agent 可用子集**：10 个同名工具，主机/会话查询、脱敏读屏、命令库查询/保存、打开会话、远端后台 exec、控制字符和应用内标签聚焦。所有变更需应用内逐次批准，绑定原 MCP 会话/原参数，拒绝、过期、单次消费、并发互斥和紧急停止生效。真实 SSH 测试覆盖 exec、带空格/单引号 cwd、安全 quoting、输出脱敏；风险检测移植了第一方 `command-risk.spec.ts` 的全部命令/敏感文件向量。
5. **MCP token**：默认禁用/暂停，手动启用 `127.0.0.1:41777/mcp`；独立随机 token 加密持久化，每个 POST/GET/DELETE 校验；反 Origin / Host rebinding，正文/并发/会话限额，轮换撤销会话并暂停。官方 SDK 客户端、2025-06-18 初始化、真实 GET SSE 与未授权 GET/DELETE 已验证。
6. **Shell / OSC**：官方 Dir/Text/Title、标题更新、BEL 与 OSC9/777 应用内通知；人工准备 bash/zsh OSC7 会话钩子，百分号编码路径、保留已有 prompt hook、幂等，不改 rc、不自动发送。Bash 真执行覆盖字符串/数组/尾分号/注释、中文/空格/ESC/百分号路径。OSC133 按决定留空，无自制观察器。
7. 核心生产包不依赖 UI，UI 系统操作经 Platform；架构测试通过。发现并修复了复制时嵌套存储锁导致超时、审批去重的指针比较/竞态、网络登录阻塞状态锁等问题；下面统计是修正后最终完整验证，不把开发中失败试跑混称通过。

## 真实验证

工作目录：`/workspace/NextShell/v2`。`go build ./...`、`go vet ./...`、`go test ./...`、`go test ./... -race` 均退出 0；另有 `CGO_ENABLED=0 go build ./...` 通过。

| 最终轮次 | 顶层测试总数 | 通过 runs | 叶子总数 | 失败 | 跳过 |
|---|---:|---:|---:|---:|---:|
| 普通 -count=1 | 146（145 通过） | 369 | 335（334 通过） | 0 | 1 |
| -race -count=1 | 146（145 通过） | 369 | 335（334 通过） | 0 | 1 |

两轮各 17 个测试包通过、0 包失败，另 3 包无独立测试。`runs` 是带 Test 名的 pass 事件（含父/子测试，不计 package pass）；叶子按实际存在名称的前缀关系计算，避免路径型向量名中 `/` 造成误算。唯一跳过是 `TestZshOSC7HookWhenAvailable`：本箱未安装 zsh，明确 UNVERIFIED，不假装测试通过。

Linux `DISPLAY=:17` 原生窗口+本地终端 smoke 退出 0；当前无 GPU，实际走软件渲染，有 AT-SPI bus 警告。官方终端 probe 实际输出：`dir="/tmp" text="probe-ok\n[Process exited]"`。这不等于完整视觉/输入法/真机验收。

证据：`/workspace/NextShell-notes/phase3-checks.log`、`phase3-{test,race}.json`、`phase3-counts.jsonl`、`count-go-tests.py`、`phase3-native-smoke.log`、`phase3-terminal-probe.log`。替换阶段独立 gate 亦全过：117 顶层、253 pass runs、227 通过叶子，失败/跳过均 0（`ecosystem-{test,race}.json`）。

## 换了哪些库

| 从什么 | 换成什么 / 精确版本 |
|---|---|
| 手工 schema 版本流程 | `github.com/pressly/goose/v3 v3.30.0` 独立 Provider、事务 Go migrations，只升级本应用已有 v2 profile |
| 手写 MCP JSON-RPC/HTTP 会话层 | 官方 `github.com/modelcontextprotocol/go-sdk v1.8.0`；保留外层认证/权限边界 |
| 远程编辑普通 TextArea | mygo `ui/editor v0.8.3`，WriteTo 保留 CRLF、脏文本保护 |
| 外部编辑器 200ms 轮询 | Platform 适配 mygo `WatchFiles v0.8.3`，core 仅定义 watcher 接口 |
| modernc SQLite v1.44.3 | `modernc.org/sqlite v1.62.0`（Goose 模块最低依赖带动），仍纯 Go |

版本已经实际 Go proxy 解析；MCP SDK LICENSE 为 MIT→Apache-2.0 过渡，不误报单一许可；Goose MIT 已读。完整审计：`GO-ECOSYSTEM-AUDIT.md`。

原本已用：mygo/terminal v0.8.3，x/crypto/ssh+agent+scrypt v0.58.0，pkg/sftp v1.13.12，x/text v0.44.0，x/net/proxy v0.62.0，标准 AES/GCM/tar/gzip。没有另写 VT/基础绘图工具包。

未替换/未引入：远端 `/proc` / `ss` 不是本机 gopsutil 的替代场景；本机监控/ping 本来未实现，不引无用 gopsutil/pro-bing；SOCKS4 保留小握手（文档允许，x/net 不支持）；现有数据库 pin 并非自研 known_hosts 解析，未加第二套 OpenSSH 信任源。没有链接 NTrace-core / GPL nexttrace；未来若做只考虑用户自装外部二进制。老 keytar 读取建议与 alex 阶段2.5决定冲突，明确不做。

## 跳过、偏离与尚欠内容

- **不是生产云服务**：参考 handler / httptest 仅内存、单账户、容量有限；没有真实服务器、注册/成员 ACL、服务端持久化、生产 TLS/运维、DEK 撤销轮换。没有外部条件，不假接旧 v1 服务。当前上传全量 snapshot，无增量/分页优化；客户端服务地址/后台开关不跨重启自动恢复。
- 自动字段合并、keep-local rebase、分叉历史强制覆盖、恢复快照浏览/恢复 UI 暂跳过：合并和回滚策略复杂，默认停在冲突保留数据更安全；普通并发冲突及明确采用远端已完成。
- **OSC133 / Agent 前台、本地执行、命令块历史、用户 pending-input 检测** 依赖上游事件，继续 `deferred, waiting on upstream`；history 返回不可用/空，而非伪造。screen 标签当前取官方 Text 的屏幕+scrollback 尾部，并非精确 viewport；近期主机排序/部分参数和返回字段未完全复刻 v1，这是已知功能缺口。
- 不移植 v1 Auto 无审批模式，全部变更保守审批；应用内 focus 不保证 OS 前台唤醒。旧 npm bridge 无 token 时不能直接连接；未改/发布旧 bridge，不宣称零配置兼容。当前 SDK 仅协商会话型 2025 协议，2026-07-28 无会话审批 profile 暂不做，避免丢失审批归属。
- 目录树/可关闭拖拽标签仍是官方小控件组合，完整 Outline/Tabs/Table 虚拟化重做成本较大，本轮跳过；无自制虚拟化算法，但大数据量性能仍欠验收。托盘/多窗口/主题设置不是已完成产品功能，后续用官方 API。
- OS 通知服务未接，终端通知先在应用内呈现；OSC52/5522 默认拒绝远程读写剪贴板。中文 IME（含新 editor）、Windows/macOS、托盘、多窗口、GPU、真实外部服务器兼容/长期运行和独立安全审计均 **UNVERIFIED**；zsh 同上。黑名单不是 shell sandbox，脱敏不是通用 DLP；弱加密口令仍可被离线猜测，路由 ID/长度等交通元数据仍可见。

## 合理默认的小取舍

回环限定/不跟系统代理；后台默认关且重启不自动联网；正常 30 秒、错误退避至 5 分钟；冲突不覆盖、选远端先加密备份；本地转云使用新 UUID、目录重名加数字；Agent 变更一律逐次审批，默认30秒/最多120秒，输出各512KiB；黑名单仅本次运行、逐行子串或 `re:` 正则，坏正则保留旧规则；MCP 轮换后须手动恢复；Shell 钩子只准备不注入。没有需要 alex 再决定才能继续的本轮阻塞；按要求在阶段3完成后停止。

协议/边界细节见 `PHASE3-PROTOCOL.md`。报告副本：`/workspace/NextShell-notes/phase3-report.md`。
