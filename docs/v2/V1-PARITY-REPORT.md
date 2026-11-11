# v1细节对齐 / 打磨报告

时间：2026-11-11 15:38（Asia/Shanghai）。v1基线 `3c5e5a8`；本轮起点 `cfd93f9`；经验证的产品代码HEAD `44dad0a`。

## 结论与审计范围

已完成第一轮高频交互打磨；不是全量v1细节已齐。用本项目固定1.x源码审计101条页面交互、全部118个IPC能力及全部41个默认设置字段；1137条原始renderer交互候选保留索引，尚未逐动态分支/逐字提示穷尽验收。

|分开统计，不重复相加|一致|有差异|缺失|按既定决策不做|合计|
|---|---:|---:|---:|---:|---:|
|页面交互（修改后）|34|49|13|5|101|
|IPC能力（不是Electron wire兼容）|55|33|15|15|118|
|1.x默认设置字段|10|23|6|2|41|

页面交互修改前为24一致/34差异/38缺失/5不做。`一致`仅指该行列出的核心行为等价，不代表布局、内部架构或每个错误分支完全相同。具体边界见PARITY.md。

## 修了哪些

- 官方原生连接/目录/标签/文件右键菜单；选区外右击不误作用旧选区；连接重命名Modal、复制IPv6地址、配置副本、导出/云页目标、回收站确认；目录递归选中用于批绑。补tags/备注编辑与侧栏搜索。
- 终端焦点内1..9、翻页/括号切标签、复制/关闭快捷键，裸Ctrl+W仍传shell；清屏/清缓冲/恢复自动标题。不是全局MRU实现。
- `preferences_v2`本地持久化，默认值叠加、校验和缓存；外观、字体/14字号/1.2行高、配色、光标/闪烁/字节缓冲、OSC策略、本地shell、目录/外部程序、keepalive、nexttrace路径。官方Form/Field对齐并修复点标签不聚焦输入的问题；沿用设计token。
- 字体/颜色实时应用；光标/缓冲/OSC剪贴板只新建或重连生效。Keepalive仅dial时解析，重连不冻结继承策略。
- 链接完整目标默认确认，经Platform处理，仅允许无内嵌凭据的http/https；取消不打开。真实外部URL未打开。
- SFTP按被右击行操作，复制路径/默认下载、源/目标可编辑确认、O_EXCL新建空文件不覆盖；终端cd正确单引号并拒绝控制字符；自动下载拒绝服务端路径穿越名称。
- 队列五状态计数、筛选、清完成/取消全部、平均KiB/s/耗时；保留失败重试，缺总量不编%/ETA。
- 命令库搜索/组筛选、复制/副本、具体名称删除确认、历史单删/确认清空；移植v1实际`[#word]`模板（非`${变量}`），按ASCII word去重、单次原样替换、逐项填写和预览。参数历史不保存。
- 官方Table进程/网络面板、进程排序/搜索/选行PID/复制命令，网络按PID/端口/IP/进程过滤；官方CPU/内存meter，首帧不造数；采样时区为CST（Asia/Shanghai）。
- Ping使用真实proxy版本`github.com/prometheus-community/pro-bing v0.9.1`；Traceroute只subprocess调用用户安装的nexttrace，带取消/30秒/64KiB限制，无shell，无GPL链接；给CLI传禁GeoIP/RDNS/地图参数，不集成PoW。仅接受IP，不隐式DNS。

## 本地提交

- `f9b3974` feat(ui): polish native context actions and persist terminal preferences
- `ebc5acc` feat(workbench): refine transfers commands and native monitor panels
- `3087b49` feat(diagnostics): add pro-bing and optional isolated nexttrace command
- `00ba86c` fix(connections): preserve reconnect policy and refine metadata and folder actions
- `96a712c` fix(terminal): confirm complete link targets through Platform boundary
- `407e9ab` fix(ui): align native form labels without losing accessible focus
- `44dad0a` fix(sftp): reject traversal names in automatic local download targets

全部只在v2本地提交，无push/PR；未动原来stash `pre-existing-uncommitted-before-v2`。未读finallyShell或其他SSH客户端代码。

## 真实测试

`go build ./...`、`go vet ./...`、`go test -count=1 ./...`、`go test -race -count=1 ./...`与`mygo_noinspector` release build全部exit 0。core/UI边界回归通过。

|命令|顶层总数/通过|通过runs（含父测试/子测试）|leaf总数/通过|失败|跳过|通过包数|
|---|---:|---:|---:|---:|---:|---:|
|普通test|191/189|474|431/429|0|2|20|
|race|191/189|474|431/429|0|2|20|

- 两次各跳过2：box禁止非特权ICMP（`ping_group_range=1 0`，uid1000、无CAP_NET_RAW）；未安装zsh。没有提权、修改sysctl或把Ping权限错误假报通过。早期真实Ping尝试确实permission denied，最终测试诚实SKIP。
- 21/21既有localhost生产功能探针通过：SSH密码/密钥、主机指纹拒绝、SOCKS4/5、PTY中文/resize/top、OSC7/title、SFTP/队列/打包/跨服普通文件、编辑冲突、监控、命令、本地终端、v1 plain/encrypted/FinalShell、v1 TS反向解析导出、参考HTTP云同步冲突、MCP token/批准/轮换、外部编辑watch、仅自己创建的进程TERM、SQLite重开。
- 原GUI测试profile重开仍可解密两类登录秘密，错误密码被拒绝。没有seed/setup或重建profile。
- 测试证据：results/gates.txt、test.jsonl、race.jsonl、test-counts.jsonl、localhost-probes.log、profile-reopen.log。Test/leaf/runs分别计数，不把runs冒称独立用例。

## 跳过/差异及原因

- 完整1137候选逐支/逐字审计、v1多scope tabs、批量失败汇总、完整快捷键/上下历史、CtrlTab MRU释放状态机：本轮范围较大，记录差异而非冒称完成。快捷键刻意仅终端焦点，避免抢输入框/IME。
- 原生文件/目录picker、远端多选剪切粘贴、跨服目录打包、递归删除、完整队列源目标/total/ETA/本地打开目录、多文档编辑/全局脏状态、命令组CRUD/参数历史、网卡选择/详情/历史曲线、回收站一键清空、密钥用途统计等尚未补齐，见逐项表。未移植危险递归删除。
- v1设置未支持项在新版设置页明确列原因，不保存假开关；背景材质/xterm WebGL/旧弹窗列宽不是mygo同构项。保留新视觉，不硬复刻旧CSS与窗口弹窗。
- Ping真实收包受内核权限阻挡；nexttrace未安装，仅fake可执行文件测参数、取消、丢弃超量输出。真实CLI及其用户配置联网行为UNVERIFIED，未连外部主机/服务。Traceroute目前完成后整段输出，不是实时逐hop流。
- 原生快速输入延迟/掉字、真实中文IME、系统剪贴板和外部链接OS失败提示仍需人工复验；离屏顺序测试通过不证明原生问题已解决。Windows/macOS均UNVERIFIED。
- 不做旧DB/device-key/keychain迁移、XOR、v1云工作区互通、OSC133自写观察器；托盘/更新/多窗口留alex阶段4。Cloud继续只有新version2协议与参考服务端集成，不是接通生产服务。

## 小取舍

- OSC52读写默认拒绝；可选写、可选逐次官方读确认。URL仅http/https、拒绝userinfo/本地远端路径/自定义协议。
- 默认下载目录必须存在；通过按钮显式应用偏好，绝不静默覆盖输入路径。参数只内存，不新增敏感参数明文历史。
- Scrollback单位MiB（1..256），不是行数；光标/缓冲策略不会暗中重建活会话。
- 新建默认继承全局keepalive true/15s；连接覆盖优先。已有明确30s旧v2配置不被改写。
- 保留Linux CJK/系统等宽字体选择，默认终端随新版亮暗；支持v1五色预设，不回退新版设计。
- traceroute不自动安装、不链接NTrace-core、不尝试提权，旧CLI不认识参数直接报错不隐式重跑。

## GUI与截图

- 新release已在DISPLAY=:17保持运行，PID `447556`；2026-11-11 15:37（Asia/Shanghai）重启。二进制 23,900,824 bytes，首次窗口观测 208.65 ms，3秒RSS 81852 kB（只作本次观测，不冒充稳定基准）。
- 路径：/workspace/NextShell-notes/v1-parity/。20张parity-{light,dark}-*.png是官方UI.Tester离屏（localhost SSH/SFTP/监控真实数据），涵盖空状态、SSH、文件、连接、设置、命令、监控、进程、网络、诊断。
- desktop-final.png为真实桌面只读截图；没有用xdotool/伪造原生点击。GUI-RETEST.md为逐点复验清单（含启动/重启、私密测试登录信息文件路径，未写密码）。设置页较长，需要向下滚动保存。
- 优先人工复验：原生快速输入与IME、右击选区作用域/目录批绑、标签快捷键和OSC手工名优先、设置live/重启及新会话策略、SFTP撞名/默认下载、命令参数预览/执行、队列重试保留、进程排序/过滤、链接取消不打开。
