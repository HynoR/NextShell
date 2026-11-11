# Go 生态审计与替换

日期：2026-11-11（Asia/Shanghai）。已完整阅读 `/workspace/nextshell-plan/GO-ECOSYSTEM.md`；按 alex 阶段 2.5 / 3 的后续决定覆盖文档中的旧建议。

## 已替换（单独提交）

- 手工 schema 版本流程 → `github.com/pressly/goose/v3 v3.30.0` 的独立 Provider、事务 Go migrations；保留应用专有 JSON 数据转换。只升级**本应用 v2 profile**，绝不读取 v1 数据库。Goose 版本 1/2/3 与 profile `PRAGMA user_version=3` 含义不同。兼容本开发分支已有 v2 profile；失败迁移事务回滚，重开不重复应用，保留原有设备密钥/密文。
- 手写 MCP JSON-RPC/HTTP 会话实现 → 官方 `github.com/modelcontextprotocol/go-sdk v1.8.0`。SDK 负责 Streamable HTTP、SSE、协商、消息和会话；外层仍须自行做每请求 bearer、回环地址/Host/Origin 检查、请求/会话上限、撤销与 kill switch。因审批必须绑定会话，目前明确协商 2025-11-25 / 2025-06-18 / 2025-03-26，未支持 2026-07-28 无会话交互。真实官方 Go SDK 客户端与 2025-06-18 初始化协议已测试。**不宣称原 v1 npm bridge 零配置兼容**：旧 bridge 没有 token 时必须调整调用端 Authorization 配置，未修改/发布它。
- 远程编辑普通 `ui.TextArea` → mygo `ui/editor`（v0.8.3），语言推断、官方编辑缓冲区；通过 `Buffer.WriteTo` 保留 CRLF，脏文本保护仍在。
- 外部编辑器 200 ms 轮询 → Platform 注入 `mygo.WatchFiles`（v0.8.3）；core 只定义 Watcher 接口，不依赖 UI，不叠加 fsnotify。真实 SFTP + 原生 watcher 测试覆盖多次回传、冲突和私密恢复文件。
- Goose 的模块最低版本带动 `modernc.org/sqlite v1.44.3` → **v1.62.0**，保持纯 Go 和 `database/sql`。

以上模块版本已通过 Go proxy 的真实模块解析核实；打开并核对了 Goose MIT LICENSE 和 MCP SDK LICENSE。SDK 正在 MIT → Apache-2.0 过渡，新代码 Apache-2.0，未再授权的旧贡献保留 MIT；不能把它笼统称为单一 MIT。发行时仍需完整许可清单。

## 本来已采用

- mygo v0.8.3 原生 Row/Column/Button/Scroll/Input、terminal 插件、本地 PTY、SSH Conn、官方拖放、单主窗口 API；没有另一套 UI 框架、VT 仿真器或自研基础绘制工具包。
- `golang.org/x/crypto v0.58.0`：SSH、agent、scrypt；标准库 AES/GCM/随机数/归档。
- `github.com/pkg/sftp v1.13.12`；`golang.org/x/text v0.44.0` 编码；`golang.org/x/net v0.62.0` SOCKS5。
- 远端 Linux `/proc` / `ss` 监控适合当前架构，不能以本机 gopsutil 代替。

## 跳过 / 不适用

- **老 keytar 读取不做**：与 alex 已定的“无 v1 DB / device-key / keychain 迁移，仅导出文件导入”冲突。不是 mygo 能力缺失；不重新加入任何 stub。
- 连接树/可关闭、可拖拽重排的会话标签仍用官方小部件组合，未全改成 Outline/Tabs/Table。官方 `ui.Tabs` 不直接替代现有 close/drag 行为；完整状态/交互重做代价较大，此轮跳过。没有手写虚拟化算法；大规模目录/表的性能优化仍欠缺。
- 当前指纹策略是数据库 pin + 显式首次信任，含 1.x 导出指纹多格式识别，不是手写 `known_hosts` 解析器。`ssh/knownhosts.New` 不能直接替代此策略；未追加 OpenSSH known_hosts 文件入口，以免引入第二套互相冲突的信任源。SSH 协议/密钥校验仍由 x/crypto 处理。
- 小型 SOCKS4/4A 握手保留（文档明确允许）；x/net/proxy 不支持 SOCKS4，旧库标签停滞，不为此引入。
- gopsutil v4、pro-bing：当前没有手写本机监控/ping 功能可替换，未塞无用依赖；本机监控、ping、traceroute 功能尚未实现。没有链接 NTrace-core / GPL nexttrace。日后若做 nexttrace，仅可选调用用户自行安装的外部二进制。
- `mygo/netproxy`：本阶段云客户端强制回环、不代理、不连真实服务；跟随系统代理会破坏测试边界，因此不引入 core/UI 耦合。公网/HTTPS 服务与代理策略留后续。
- 自动更新、i18n 在阶段 4 才用官方能力；不引社区 UI、第二套托盘/更新器/钥匙串库。没有采用文档中标“未核实”的 go-winio、社区 UI 或压缩库。
- 托盘、多窗口、主题设置不是已完成的产品功能：当前只有单主窗口，没有自研替代；后续应直接采用官方 API。中文 IME、macOS/Windows、真实托盘/多窗口仍 UNVERIFIED。OSC 133 仍无公开事件 API，继续 deferred, waiting on upstream；没有自写流观察器。

## 替换提交的独立验证

`go build ./...`、`go vet ./...`、普通及 `-race` 全量测试均成功。替换完成时两轮各有 117 个通过顶层、253 条 test pass 运行、227 个通过叶子，0 失败 / 0 跳过（按实际存在测试名的前缀判断父子；用最后一个斜杠判断会把带路径的向量名误算成叶子）。日志：`/workspace/NextShell-notes/ecosystem-{test,race}.json`。后续再显式关闭 Goose 全局 migration 注册表，Provider 只采用本包给定的三个 migration；最终阶段 3 验证覆盖该隔离选项。
