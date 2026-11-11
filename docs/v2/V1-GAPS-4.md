# 四项补全：调研与实施记录

## 选型（基于实际 v0.8.3 源码）
- MRU：mygo `ui/context` 的 Shortcut、Modifiers、官方 TabsBase；`ui/input.go` 的 modsChanged 明确在修饰键变化时重绘。自行实现小型、纯 Go MRU 状态机（不是控件/虚拟列表）；对照本项目 v1 `hooks/useSessionTabShortcuts.ts` 的冻结快照、Ctrl 松开提交规则。
- SFTP：官方 `ui.ListState.Selection` / `ui.Selection[string]` 支持 Ctrl/Shift 多选，`ui.Table` 复用 ListState。协议层继续用 BSD-2-Clause `github.com/pkg/sftp v1.13.12`。多路径剪贴板、递归调度属应用业务，不引入另一套文件系统/GUI；优先 SFTP 原生 Rename，跨服务只复制。
- 编辑器：官方 `ui/editor.State`、Buffer 与 TabsBase；每文档独立持有状态，保留撤销、选区和滚动，不手写编辑器。参考 `docs/ui/code-editor.md`。
- 曲线：**mygo 自带 `ui/chart`**（chart.go / shape.go / time.go / scale.go），原生 Plot / Line / Time，无需第三方图表。核心只保留有界数值历史，不依赖 UI。
- 官方文档：`/home/box/go/pkg/mod/github.com/egoist/mygo@v0.8.3/docs/ui/{list,table,tabs,code-editor,chart}.md`；实际 API 源码 `v2/third_party/mygo/ui/`（MIT，既有审计 fork）。不新增许可证依赖。
- v1 核实：`SystemInfoPanel.tsx` 仅网络速率绘制 50 点历史，CPU/内存/磁盘原本是即时指标；本次为这些已有指标增加历史，磁盘指容量占用，不冒称磁盘 I/O。

## 实施结论（2026-11-12 00:36，Asia/Singapore）

四项都已补上可用实现，未新增第三方依赖，未改 mygo fork。不是声称所有平台/所有边界已实机验收。

| 功能 | 结果 / 验证 | 边界 |
|---|---|---|
| MRU 标签 | 已完成：冻结最近使用快照、Ctrl+Tab / Ctrl+Shift+Tab、按住逐项预览、Ctrl 松开才激活一次；方向键；Esc/窗口失焦取消。纯 Go 状态机 + 官方 Shortcut/Modifiers/WindowActive/TabsBase | 显示紧凑原生预览条，不照搬 v1 DOM 浮层；真实桌面松键/焦点待人工复验 |
| SFTP 多选、剪切/复制/粘贴、目录跨服务复制 | 已完成：官方 Table.Selection，Ctrl/Shift/Ctrl+A；表格 Ctrl+C/X/V、工具条和右键入口；同服务原生 Rename；目录递归通过 pkg/sftp 流式传输并在私密暂存目录完成后发布；每选中项一个可取消/重试的队列任务 | 不跟随/复制符号链接或特殊文件，不递归覆盖/合并已有目录；最多 10000 项/64 层；不复制所有者/ACL/xattr。剪切跨会话要求地址、账户、代理、非空已验证 pin 完全匹配；不确定身份只允许复制 |
| 编辑器多文档 | 已完成：每文档独立官方 State/Buffer，原生文档标签、脏标记、保存、关闭确认、跨文档保留选区/撤销/滚动；异步保存回包按会话+路径路由，不抢其他文档焦点，不覆盖保存后的新输入；原有 CRLF 测试继续通过；应用退出显示脏文档数 | 最多 32 文档，仅内存草稿；“保存并保留文档”明确不自动关闭，成功后可再关，避免异步保存误丢后续输入 |
| 监控历史 | 已完成：官方 chart.Plot/Line/Time；CPU、内存/Swap、全部/指定网卡接收发送、选择挂载点的容量百分比；真实采样，首样本缺 delta 不造零；计数器复位/网卡变化断线；>10 秒采样缺口断线；悬停数值 | 每会话最近 50 点，最多缓存 32 会话，内存历史；磁盘是容量占用，不是未实现的读写 I/O；v1 原本仅网络曲线，本轮其余为既有指标的扩展 |

### 重要实现取舍
- v1 `useSessionTabShortcuts.ts:135–190` 明确循环只改变预览，Ctrl 松开才激活；据此增加最终对齐修复，避免一轮循环反复重放/激活终端。
- v1 `services/sftp-service.ts:718–762` 的跨服务器目录流程依赖两端 tar、源端打包再落盘中转。本轮改用现有 BSD pkg/sftp 递归流，避免 tar/远端 shell 依赖与本地中转明文；原有打包上传/下载保持可用。
- 文件目录改成双击/Enter 打开，单击用于选择；更新原有回归测试并实测 Ctrl+点击和 Ctrl+C。这是有意 UX 调整，不是按钮失效。
- 剪贴板是应用内结构化远端路径，不污染系统文本剪贴板；保留失败剪贴板。成功剪切再次粘贴会安全报源不存在，不重复删除。
- 剪贴板粘贴默认不覆盖、不合并；原有“跨会话传输”的**显式单文件覆盖**仍保留并单独回归。
- 失败只清理本次创建的随机私密暂存路径，清理前恢复自建只读目录权限；永不清空用户目录。
- 未加入第三方图表/编辑器/虚拟列表；只手写状态机、远端剪贴板/文件操作编排和有界数据历史这些应用业务。

## 本地提交
- `67acae7` 冻结 MRU 状态机与官方键盘/标签集成。
- `0541bf6` 官方 SFTP 多选、结构化剪贴板、安全递归复制及真实 SSH/SFTP 测试。
- `cb85691` 多文档官方编辑器、脏关闭确认、异步保存保护。
- `52d119d` 跨会话剪切要求匹配的已验证 pin，避免仅按地址猜测服务器身份。
- `db5f6b7` 核心有界历史、独立网卡 delta、官方原生曲线。
- `47cbe59` MRU 严格对齐 v1：只预览、松 Ctrl 才激活、失焦取消。
- `f762969` 保留显式单文件覆盖、只读暂存目录清理与键盘回归。
- 最后另有报告/复验清单文档提交；实际测试的代码 HEAD：`f7629693a5a27e81fba155294cef7f698015e292`。

## 真实验收结果

Go 1.27.3 / linux-amd64；`go build ./...`、`go vet ./...`、`go test -count=1 -json ./...`、`go test -race -count=1 -json ./...` **全部 exit 0**。发布版 `-tags mygo_noinspector -trimpath -ldflags='-s -w'` 成功。

| 计数口径 | 普通测试 | -race |
|---|---:|---:|
| 顶层测试（含跳过） | 209 | 209 |
| 顶层通过 | 207 | 207 |
| 通过的 runs（含父测试与子测试，不是独立 leaf） | 492 | 492 |
| 最底层 leaf（含跳过） | 449 | 449 |
| leaf 通过 | 447 | 447 |
| 失败 | 0 | 0 |
| 跳过 | 2 | 2 |
| 通过包数 | 21 | 21 |

较上轮增加 18 个顶层测试。既有两个环境跳过：pro-bing 的本机 ICMP 权限不足；zsh 未安装。**不把跳过算通过**。

- 原有生产 Platform localhost 功能探针 **21/21 可用**（真实 PTY、SSH/SFTP、打包传输、导入导出、参考云端、MCP、重启持久化等）；没有连接外部真实主机或云服务。
- 新增真实 localhost + 官方 ui.Tester 的明/暗两轮流程均成功：MRU 预览/松键、多选、队列目录复制/剪切、双文档保存/草稿、6 次真实监控采样。目录跨服务器核心和生产队列另通过**两套独立 in-process SSH/SFTP 服务**验证，不把同主机双会话假称不同生产服务器。
- 回归包括：MRU 冻结/反向/取消/关闭 ID、真实 Ctrl/Shift 键事件与失焦；递归复制内容/进度/冲突/取消/符号链接拒绝/清理；多文档 dirty/会话身份/保存期间继续输入/后台保存不抢焦点/关闭取消；50 点上限、缺数据、乱序采样、网卡复位/拓扑变化、会话隔离、原生曲线渲染。
- 原始产物：`gaps-results/{build.log,vet.log,test.jsonl,race.jsonl,counts.jsonl,localhost-probes.log,new-feature-probes.log,manifest.json}`；计数脚本 `/workspace/NextShell-notes/count-go-tests.py`。

## 发布、截图与人工复验
- 已启动并保持 GUI：DISPLAY `:17`，PID `524558`，沿用原 `linux-test/profile/nextshell.db`，保留人工临时连接/目录，没有重建/清空 profile。旧记录 PID 447556 先已退出；确认 /proc 无该进程、无 NextShell 窗口后，用原 launch 脚本启动新 release，没有强杀其他进程。
- 二进制 `linux-test/bin/nextshell`：24,032,696 字节；运行中 /proc/exe 与新 release SHA-256 完全一致：`b4bf6e9d819d94aa6bdb9aec9f034eac2ad8dafc5b9512a32f5173b621b50d30`。
- 原脚本观察窗口映射约 209.99 ms，启动 3 秒 RSS 82,292 kB；这是单次冷/热缓存未控制的观察，不是性能基准。
- 新功能明/暗各四图：`gaps-shots/gaps4-{light,dark}-{mru,multiselect,documents,history}.png`（官方离屏 ui.Tester，数据为真实 localhost；不是桌面输入自动化）。另有 8 张基本页面图。
- 真桌面截图 `gaps-shots/desktop-final.png`，只读抓取 DISPLAY=:17，确认实际窗口在运行；未用注入桌面输入。
- 人工清单已追加四项详细步骤：`/workspace/NextShell-notes/v1-parity/GUI-RETEST.md`。重点：Ctrl 松键/失焦、多选快捷键、异步保存后继续输入、脏文档关闭。

## 未验证 / 保留缺口
- 原生桌面的实际物理键盘、IME、焦点切换、选区/撤销/滚动手感仍需人工；官方 headless 测试通过不能冒充这些实机通过。以前的原生输入延迟/丢序问题仍不据此宣布修好。
- Windows/macOS 真机、高 DPI、多显示器、多窗口、超大目录/长时间传输/断电故障未实测；目录 10000 项/64 层以上明确拒绝，不承诺处理。
- 符号链接/特殊文件复制、已有目录合并、跨不同账户/不明身份的剪切、磁盘 I/O 历史、跨重启未保存草稿恢复没有实现，原因及保守默认见上表；本轮没有卡住而整项跳过的功能。
- v1 DB 迁移、v1 云工作区互通、XOR、OSC 133 的既定不做/等上游决策不变；托盘/更新/多窗口仍留给 alex 的阶段 4。

所有源码变更只在本地 `v2` 提交；无 push / PR。没有读取 finallyshell 或其他 SSH 客户端源码。

