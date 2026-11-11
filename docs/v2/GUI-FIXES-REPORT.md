# Linux GUI 验收问题修复报告

日期：2026-11-11，Asia/Shanghai（UTC+8）。分支 v2，仅本地提交，无 push/PR，无外部真实服务访问。

## 结论

剪贴板快捷键/发布版调试面板、回收站确认、OSC 标题、中文终端字距、known_hosts 可选读取及命令执行后的可见性已修复并有回归测试。标签重命名增加稳定控件标识和手动标题优先规则，但原人工报告的失败在自动测试器里未复现，仍需实点复验。**原生 Linux 输入卡顿、丢字、乱序尚未定位或证明修好；不能把这轮描述为全部问题已经解决。**

## 本地代码提交

- `986e9f9`：隔离开发 inspector；发布版显式禁用；限制开发快捷键；终端剪贴板/FIFO 回归测试；mygo v0.8.3 小范围本地补丁。
- `ba54f33`：标签/OSC 手动名称规则、永久删除可见对话框、命令执行跳回终端、token 成功提示；可选只读 known_hosts；GUI 与输入顺序测试。
- `7a1d8dd`：Linux 已安装 Noto Sans Mono CJK SC 时，使用匹配终端网格的字体尺寸；字形度量回归测试。
- 本报告另作本地文档提交（commit hash 见最终回报）。

提交仅使用命令级本地 executor 身份；没有修改全局 Git 身份、丢弃用户文件或动既有 stash。

## 逐项根因、修法与证据

| 项目 | 发现 / 根因 | 处理与状态 |
|---|---|---|
| Ctrl+Shift+C/V / Elements | mygo inspector 默认键盘入口截获 Ctrl+Shift+C，把终端焦点带进调试 UI；开启该面板后也影响粘贴。 | NextShell 普通 build 显式 DevToolsDisabled，忽略环境变量；验收 release 额外用 mygo_noinspector 标签将面板编译移除。开发必须同时使用 nextshell_devtools 构建标签和 NEXTSHELL_DEVTOOLS=1，唯一入口 Ctrl+Alt+F12。官方 terminal 的复制、粘贴、700 字节连续输入顺序测试通过。原生系统剪贴板跨应用仍待人工复验。 |
| 标签重命名 | 修复前 ui.Tester 真实输入路径即可成功，不能断言已找到原人工失败的根因。 | 给标签名称/按钮稳定 key 与语义 label；显式 invalidate；手动标题标记独立保存，重连继承。逐字输入、重命名按钮及通用中文输入回归通过；需原生 GUI 复验。 |
| 回收站永久删除 | storage.Delete 原本就物理删除资源和凭据；“永久删除”按钮仅设置待确认状态，确认控件却放在长连接表单底部、不可见，用户动作没有到达 Delete。 | 改为根级官方 ui.AlertDialog，可见的取消/确认；点确认后走原生产 Platform/SQLite Delete。测试实际检查快照消失；localhost CRUD/trash/purge 也通过。云同步 tombstone 保留是必要协议行为，不是回收站残留资源。 |
| 输入卡顿/乱序 | immediate-mode 每次事件可能重建 UI 树，terminal 行绘制有缓存；查看正常终端按键链路未见每键数据库或网络 IO，terminal 写入使用 FIFO。误开 inspector 的确增加重绘成本，但尚不能证明它解释了所有原生乱序/丢字。 | 加 native-text-client commit、批量事件、terminal FIFO 回归和 CPU profile；未找到可证实的生产输入顺序 bug，未臆测改队列。仍未解决原生症状；详见下节。 |
| OSC 0/2 标题 | terminal 官方 API 已读到标题，但 App 标签没有把它同步过来。 | 每帧经 Platform.TerminalTitle 更新未手动命名的标签；手动名称胜出，重连保留。OSC 0、OSC 2、手工覆盖、重连测试通过。 |
| 中文字间距 | box 默认 monospace 13：ASCII 网格 8px，CJK 实际字形 13px，而双宽单元预留 16px，多出 3px；不是把中文算成错误格数。 | Linux 有 Noto Sans Mono CJK SC 且未指定字体时用该字体 14px：ASCII 单格 7px，CJK 14px，双格间隙 0px。实际官方字体 shaping 度量通过。不安装新字体；其他平台/缺字体环境保持原回退，UNVERIFIED。 |
| OpenSSH known_hosts | 以前只支持数据库 pin，未消费 OpenSSH 文件。 | 新连接选项 UseKnownHosts，默认关闭；仅打开 ~/.ssh/known_hosts 读取，使用 x/crypto/ssh/knownhosts。DB pin 优先；无 pin 才检查文件，匹配放行、变更/撤销拒绝、未知主机仍走显式确认。文件缺失回到原确认策略，其他读取/解析错误拒绝；从不写该文件。真实 Go SSH 服务器测试匹配、变更、DB 优先和文件字节不变。 |
| 命令库当前会话执行 | 原生产 Platform 执行已有输出；UI 留在命令编辑页面，输出在其他页不可见。 | 成功发送后跳到终端并给提示。现有生产执行测试及真实 localhost probe 均通过；需原生点击复验可见性。 |
| token 轮换勾选框 | 修复前真实 tester 点击即能切换，没有证据证明 checkbox 有 bug。 | 保留切换/轮换后清空逻辑，补实际点击回归和明确的轮换完成、Agent 暂停提示。不把原先的 UNVERIFIED 谎称已定位的缺陷。 |

## 输入性能：真实样本及边界

日志位于 `/workspace/NextShell-notes/linux-test/fixes/`。以下均为 **mygo 内存 UI.Tester**，不是 xdotool/GTK/X11 端到端测量，也不是物理按键到屏幕的延迟。

| 同一 200 字符输入 probe | 修改前 | 修改后 |
|---|---:|---:|
| tester 合成输入总时间 / 每字符 | 24.952ms / 0.1248ms | 24.265ms / 0.1213ms |
| text-client commit 总时间 / 每字符 | 22.839ms / 0.1142ms | 20.157ms / 0.1008ms |
| 无需额外事件即发布 / 最终字符数 | 200/200；200 | 200/200；200 |

单次轻量样本，不据此宣称修好输入性能。另在完整 App 里测同样 200 个字符：

- inspector 关闭：53.172ms，p50 0.234ms、p95 0.405ms、max 1.200ms。
- inspector 打开：103.670ms，p50 0.492ms、p95 0.634ms、max 1.514ms。
- 两种情况字符串均逐字完全一致；批量中文输入、终端 700 字节 burst 也无丢失/乱序，修复前同样未复现。

完整 App 开面板的样本成本约为关闭时两倍；release 禁用可消除该已确认的额外负担，但不能证明原生输入问题已根治。短 CPU profile 主要见绘制/文本/native 调用，采样含初始化与字体加载，不能据此归咎软件渲染或上游。曾怀疑 text-client commit 缺少 invalidate，进一步检查发现 adapter 原本已请求帧、baseline 测试也通过，因此撤销了这个不成立的生产补丁。

本轮只有内存测试器可做可重复输入，没有可用的受支持原生桌面输入驱动，因此未进行 xdotool 系统端到端压测。**原生快速键入必须继续人工复验，并保留为未解决项；当前没有足够证据向 mygo 上游归责。**

## 构建与真实测试数量

对最终代码运行（全部 exit 0）：

```sh
go build ./...
go vet ./...
go test -json -count=1 ./...
go test -race -json -count=1 ./...
go test -tags nextshell_devtools ./cmd/nextshell
go build -tags mygo_noinspector ./...
```

| 全量 Go 测试 | 顶层总数 / 通过 | 通过 runs（含子测试） | leaf 总数 / 通过 | 失败 | 跳过 |
|---|---:|---:|---:|---:|---:|
| 普通 -count=1 | 158 / 157 | 384 | 349 / 348 | 0 | 1 |
| -race -count=1 | 158 / 157 | 384 | 349 / 348 | 0 | 1 |

18 个有测试包均通过。唯一 skip：TestZshOSC7HookWhenAvailable，box 没有 zsh；没有把 skip 当成通过。上表两次分别统计，不混加为“768 个不同测试”。开发构建的 cmd 测试另行通过；核心 16 个包的传递依赖检查均无 mygo/UI（排除 appui、desktop 两个桥接包）。

真实本机 SSH/PTY/SFTP、SOCKS、传输/编辑、导入导出、参考云服务器、Agent/MCP、持久化验收探针另有 **21/21 可用**，不混入 Go 测试计数。GUI profile 重开后密码/密钥均可解密登录，错误密码拒绝。没有连接外部真实服务。

证据：test.json、race.json、counts.jsonl、targeted.log、input-before.log、full-ui-timing.log、full-ui.cpu、full-ui-pprof.txt、live-probe.log、reopen.log、core-dependencies.json。嵌套 third_party/mygo 是依赖模块，未把被省略的上游测试套件计入上述统计；本次补丁由 NextShell appui/cmd 回归覆盖。

## 发布实例、默认取舍与未验证

- `bin/nextshell` 已用 `go build -trimpath -tags mygo_noinspector` 从代码提交 7a1d8dd 重编译；原实例先按 PID+exe 校验后退出，再复用原 profile 重启，没有重置人工数据。
- 截至 2026-11-11 12:54（Asia/Shanghai），GUI 在 DISPLAY=:17、PID 365428 存活；路径 `/workspace/NextShell-notes/linux-test/bin/nextshell`。观察窗口映射 207.1ms，启动 3 秒后 VmRSS 78,064KiB（约 76.23MiB）；这些不是输入延迟指标。二进制 33,039,032 字节，约 31.51MiB。
- 截图：`/workspace/NextShell-notes/linux-test/fixes/desktop-fixed.png`。人工重点清单：同目录 `GUI-RETEST.md`。启动/重启仍用上一轮 launch-gui.py / stop-gui.py，登录信息只存 private/login.json，不在报告写密码/token。
- mygo 仍固定 require v0.8.3；为了不让依赖默认调试键盘逻辑劫持终端，采用带 MIT 许可证、约 21MiB 的最小用途裁剪本地副本 + replace，生产改动仅 inspector.go，另两个文件仅扩展测试器。来源/hash/范围见 third_party/mygo/NEXTSHELL-PATCHES.md。没有阅读竞争 SSH 客户端项目。后续需随 upstream 升级复核这份小补丁，不能宣称未修改官方依赖。
- known_hosts 默认关闭、缺文件保留旧 TRUST/DB pin 流程、存在但解析失败则拒绝；v1 导出格式没有 UseKnownHosts 字段，该本地选项不随 v1 文件导出。无 v1 数据库/钥匙串迁移。
- 不持久化标签手动名称至跨应用重启，只保证当前会话/重连不被 OSC 覆盖；原计划未要求恢复终端会话。
- 未验证：原生跨应用剪贴板/输入法/快速输入、人工重命名实际故障复现、其他字体环境、Windows/macOS 真机。OSC 133 仍 deferred 等上游，本轮未实现。
- 复验优先顺序：原生输入遗漏/顺序 → Ctrl+Shift+C/V → 重命名/OSC/重连 → 回收站对话框 → 命令执行跳转 → token 确认/轮换 → 中英文字距。
