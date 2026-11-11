# v1 / v2 细节对照（本轮完成后）

v1固定 origin/main `3c5e5a8`，v2起点 `cfd93f9`。时间：2026-11-11（Asia/Shanghai）。只读取本项目及mygo官方代码；未读其他SSH客户端。

**统计边界**：101条逐页交互与118条IPC能力分别统计，不相加冒充独立功能。另列设置默认值，亦不重复累计。`一致`表示该行的可观察核心行为等价，不是Electron wire/API、逐字提示或每个条件分支兼容。

**没有完成的审计**：1137条原始生产renderer交互候选见 SOURCE-INVENTORY.md。尚未穷尽这些候选的动态分支/逐字文案；本轮以高频页为重点，未完成部分不计为一致。协议/发布边界按既定决策明确排除。

## 统计

- 交互修改前：{'一致': 24, '有差异': 34, '缺失': 38, '按既定决策不做': 5}
- 交互修改后：{'一致': 34, '有差异': 49, '缺失': 13, '按既定决策不做': 5}
- IPC能力：{'一致': 55, '有差异': 33, '缺失': 15, '按既定决策不做': 15}，总计118。

## 页面交互

|ID|页面|v1交互/要求|状态|Go现状/限制|v1源码（spec快照相对路径）|
|---|---|---|---|---|---|
|UI-001|连接/目录|名称/主机/端口/用户、密码/密钥/agent/交互认证|一致|v2 connectionsView + sshclient；端口默认22；本轮补tags CSV与备注编辑，去重后保存|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-002|连接/目录|保存后不回显密码、留空不替换已有密码|一致|已有回归|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-003|连接/目录|目录层级、重命名、父目录|一致|v2 foldersView|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-004|连接/目录|名称/主机搜索|一致|v2 matches|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-005|连接/目录|按标签/备注/分组搜索|有差异|侧栏增加tags/notes/目录名搜索；管理表本身仍无独立完整筛选器|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-006|连接/目录|表格/网格资源视图|一致|v2 已有|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-007|连接/目录|连接右键：编辑/连接/重命名/复制地址|一致|parity_menus.go connectionMenu；原生菜单+重命名Modal，含IPv6地址回归|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-008|连接/目录|右击选区外行只作用该行；选区内多选批量操作|一致|connectionTargets；右击选区外不会误作用旧选区；导出选区回归|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-009|连接/目录|批量绑定认证|有差异|两版均逐条保存；v1继续并汇总失败，v2首错停止；跨scope约束UI也不同，未伪称完全一致|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-010|连接/目录|右键导出/复制到工作区/删除|一致|菜单按实际目标选区进入导出/新协议云页；回收站确认|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-011|连接/目录|连接删除进入回收站、有确认|有差异|新增右键删除确认；旧编辑表单移入按钮仍直接软删除，非永久删除|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-012|连接/目录|目录右键新建子目录/重命名/删除/含子目录批量绑定|有差异|新建子目录、编辑、递归选中批绑已做；仅允许删除空且未引用目录，不递归删除资源|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-013|连接/目录|回收站恢复/永久删除显式确认|一致|既有删除回归|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-014|连接/目录|回收站清空与多选还原|缺失|只有逐项|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-015|连接/目录|拖动连接进目录/目录重排|缺失|v2 仅选择父目录|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-016|连接/目录|本地/工作区 scope 管理、跨scope复制|有差异|v2 新协议可复制，无v1 scope UI|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-017|连接/目录|密钥生成/导入/用途统计/内联创建|有差异|生成导入已有；用途和内联创建未全齐|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-018|连接/目录|代理密码不回显、SOCKS4/5支持|有差异|基本能力一致，文案/布局不同|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-019|连接/目录|查看已保存连接密码需明确动作|缺失|v2 不提供 reveal UI|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-020|连接/目录|空列表、搜索无结果、错误提示逐字文案|有差异|新版视觉/提示，不声称逐字相同|apps/desktop/src/renderer/components/ConnectionManagerV2/index.tsx;utils/rowCommands.ts|
|UI-021|标签/终端|标签点击/拖动重排/关闭/复制/重连|一致|既有回归|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-022|标签/终端|标签右键：连接管理/重命名/复制/重连/关闭|有差异|原生tabMenu已做；重命名仍使用顶部输入框，不复制v1浮层|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-023|标签/终端|Ctrl/Cmd+1..9；9跳最后标签|有差异|已做但只在终端获得焦点时，避免抢输入框/IME快捷键；9=最后|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-024|标签/终端|Ctrl+PageUp/PageDown、Ctrl/Cmd+Shift+[或]|有差异|终端焦点内支持环绕；非全局|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-025|标签/终端|Ctrl+Shift+W 非mac关闭；Cmd+W mac关闭|有差异|终端焦点内支持；bare Ctrl+W仍传shell，有回归；mac真机未验证|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-026|标签/终端|Ctrl/Cmd+Shift+D复制会话|有差异|终端焦点内支持，UI回归|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-027|标签/终端|Ctrl+Tab MRU预选，松Ctrl提交，Esc取消|缺失|MRU状态机较大|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-028|标签/终端|Ctrl+Shift+C/V、官方终端右键菜单|一致|既有回归；原生跨应用仍待验|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-029|标签/终端|Ctrl+Shift+F、F3终端查找|有差异|官方终端支持查找，但应用入口/快捷键不完全相同|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-030|标签/终端|OSC0/2更新标题，手工名称优先|一致|v2 明确手工优先；v1 OSC优先，不照搬覆盖手工名|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-031|标签/终端|断线后标题恢复、重复会话序号|有差异|v2 未完整编号/复原|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-032|标签/终端|OSC7目录、SFTP跟随当前目录|一致|官方API和真SSH探针|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-033|标签/终端|OSC133 Shell prompt/command marks|按既定决策不做|等待mygo上游，不自写观察器|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-034|标签/终端|清屏/清滚动/复制当前输出快捷操作|有差异|标签菜单新增ClearScreen/ClearScrollback；复制使用官方终端菜单/键盘，不另做整屏复制入口|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-035|标签/终端|终端通知与任务栏进度|有差异|仅本地事件消息；系统通知/进度未齐|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-036|标签/终端|链接打开前确认、OSC52读写开关|有差异|完整URL默认确认；仅http/https无userinfo。OSC52默认拒绝，读可选逐次官方确认，写可选；非v1较宽松默认|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-037|标签/终端|全屏top、调整大小、中文、local shell|有差异|Linux真PTY通过，IME/vim/跨平台未验证|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-038|标签/终端|终端字体/字号/行距/主题设置入口|缺失|没有统一设置页|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-039|标签/终端|光标/闪烁/滚动缓冲设置|有差异|本轮扩展：官方Cursor/NoBlink/Scrollback，缓冲为MiB非行数；这些仅新会话/重连生效|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-040|标签/终端|连接断开/退出时对未保存编辑统一守护|缺失|外部窗口/多标签细节较大|apps/desktop/src/renderer/components/WorkspaceLayout.tsx;hooks/useSessionTabShortcuts.ts;components/TerminalPane.tsx|
|UI-041|SFTP/编辑|浏览/刷新/上级/文件与文件夹图标|一致|v2官方Table|apps/desktop/src/renderer/components/FileExplorerPane/components/FileExplorerContextMenu.tsx|
|UI-042|SFTP/编辑|右键编辑/逐个下载/打包下载/发送服务器|有差异|右键编辑/下载已做，目录下载走packed；跨服务器仍表单入口，没有右键目标选择器|apps/desktop/src/renderer/components/FileExplorerPane/components/FileExplorerContextMenu.tsx|
|UI-043|SFTP/编辑|右键复制/剪切/粘贴远端文件|缺失|需远端覆盖/目录语义设计|apps/desktop/src/renderer/components/FileExplorerPane/components/FileExplorerContextMenu.tsx|
|UI-044|SFTP/编辑|重命名/复制路径/删除右键|一致|fileMenu严格作用被右击行；操作源/目标可编辑，删除仍确认且不递归|apps/desktop/src/renderer/components/FileExplorerPane/components/FileExplorerContextMenu.tsx|
|UI-045|SFTP/编辑|空白右键上传/新文件/新目录/终端cd|有差异|新建空文件O_EXCL、目录、刷新、POSIX单引号cd已做；上传菜单仅指向表单，无原生文件picker|apps/desktop/src/renderer/components/FileExplorerPane/components/FileExplorerContextMenu.tsx|
|UI-046|SFTP/编辑|F2/Delete/F5、键盘目录导航|有差异|v2 Table Enter有，其他入口未齐|apps/desktop/src/renderer/components/FileExplorerPane/components/FileExplorerContextMenu.tsx|
|UI-047|SFTP/编辑|多选下载/删除/文件剪贴板|缺失|v2 单路径操作|apps/desktop/src/renderer/components/FileExplorerPane/components/FileExplorerContextMenu.tsx|
|UI-048|SFTP/编辑|拖入本地文件上传|一致|保留官方DroppedFiles|apps/desktop/src/renderer/components/FileExplorerPane/components/FileExplorerContextMenu.tsx|
|UI-049|SFTP/编辑|上传/下载/打包/跨服务器队列|有差异|普通文件跨服务器和各端打包已有；跨服务器目录打包缺失，未虚称一致|apps/desktop/src/renderer/components/FileExplorerPane/components/FileExplorerContextMenu.tsx|
|UI-050|SFTP/编辑|默认上传/下载目录设置与路径选择器|有差异|v2 手填|apps/desktop/src/renderer/components/FileExplorerPane/components/FileExplorerContextMenu.tsx|
|UI-051|SFTP/编辑|内置编辑/保存/脏状态/远端哈希冲突|一致|回归与真SFTP|apps/desktop/src/renderer/components/FileExplorerPane/components/FileExplorerContextMenu.tsx|
|UI-052|SFTP/编辑|内置/外部编辑默认模式/默认程序|有差异|有外部监视，无偏好|apps/desktop/src/renderer/components/FileExplorerPane/components/FileExplorerContextMenu.tsx|
|UI-053|SFTP/编辑|新建远程文件和大文件/二进制提示|有差异|新增O_EXCL空文件不覆盖回归；UTF8/8MiB限制不变，未做v1完整大文件UX|apps/desktop/src/renderer/components/FileExplorerPane/components/FileExplorerContextMenu.tsx|
|UI-054|SFTP/编辑|多文档编辑会话列表/关闭所有编辑|有差异|v2 单文档|apps/desktop/src/renderer/components/FileExplorerPane/components/FileExplorerContextMenu.tsx|
|UI-055|SFTP/编辑|未保存改动切文件确认|一致|已有回归|apps/desktop/src/renderer/components/FileExplorerPane/components/FileExplorerContextMenu.tsx|
|UI-056|传输/命令|队列运行/完成/失败/取消及重试|一致|v2核心已有|apps/desktop/src/renderer/components/TransferQueuePanel.tsx;components/command-center/CommandCenterPane.tsx|
|UI-057|传输/命令|队列运行/失败/完成数量与空状态|一致|五状态数量、空列表/筛选为空、清完成保留失败可重试|apps/desktop/src/renderer/components/TransferQueuePanel.tsx;components/command-center/CommandCenterPane.tsx|
|UI-058|传输/命令|队列状态筛选、清除已完成|一致|增加filter/ClearCompleted/CancelAll；不会移除运行或失败记录|apps/desktop/src/renderer/components/TransferQueuePanel.tsx;components/command-center/CommandCenterPane.tsx|
|UI-059|传输/命令|速度/百分比/总大小/展开源目标/打开目录|有差异|增加累计平均KiB/s/耗时；没有total不伪造%/ETA；源目标详情/本地打开目录未齐|apps/desktop/src/renderer/components/TransferQueuePanel.tsx;components/command-center/CommandCenterPane.tsx|
|UI-060|传输/命令|任务重试保留路径/覆盖策略|有差异|核心closure已有，UI信息不足|apps/desktop/src/renderer/components/TransferQueuePanel.tsx;components/command-center/CommandCenterPane.tsx|
|UI-061|传输/命令|命令新增/编辑/分组/当前与全会话执行|一致|v2已有，AppendCR保留|apps/desktop/src/renderer/components/TransferQueuePanel.tsx;components/command-center/CommandCenterPane.tsx|
|UI-062|传输/命令|命令删除前不可恢复确认|一致|原本已有确认，本轮改为具体命令名与不可恢复提示，移除重复绘制确认框；并非原本直接删除|apps/desktop/src/renderer/components/TransferQueuePanel.tsx;components/command-center/CommandCenterPane.tsx|
|UI-063|传输/命令|命令关键字搜索/分组筛选/复制|一致|按名称/组/描述/正文搜索，分组过滤、上下文复制/另存副本；UI回归|apps/desktop/src/renderer/components/TransferQueuePanel.tsx;components/command-center/CommandCenterPane.tsx|
|UI-064|传输/命令|命令[#word]模板预览/逐项填写/变量历史|有差异|核实v1实际语法是[#word]，不是${变量}；已移植ASCII word/去重顺序/单次原样替换+预览，参数历史不持久化|apps/desktop/src/renderer/components/TransferQueuePanel.tsx;components/command-center/CommandCenterPane.tsx|
|UI-065|传输/命令|命令文件夹重命名/删除带子项确认|缺失|v2仅组字符串|apps/desktop/src/renderer/components/TransferQueuePanel.tsx;components/command-center/CommandCenterPane.tsx|
|UI-066|传输/命令|命令历史单项删除/清空/上下选择|有差异|新增历史管理/单删/确认清空；未做完整上下快捷键选择|apps/desktop/src/renderer/components/TransferQueuePanel.tsx;components/command-center/CommandCenterPane.tsx|
|UI-067|传输/命令|命令空状态、未连接禁止执行、错误提示|有差异|核心检查存在，UI不完整|apps/desktop/src/renderer/components/TransferQueuePanel.tsx;components/command-center/CommandCenterPane.tsx|
|UI-068|监控/诊断|Linux系统/CPU/内存/swap/磁盘/网络采样|一致|真SSH探针|apps/desktop/src/renderer/components/SystemInfoPanel.tsx;NetworkMonitorPane.tsx|
|UI-069|监控/诊断|CPU/内存使用率视觉meter|一致|官方Progress，首帧CPU不显示假数据；总量异常/NaN钳位回归|apps/desktop/src/renderer/components/SystemInfoPanel.tsx;NetworkMonitorPane.tsx|
|UI-070|监控/诊断|进程官方表格、过滤、排序、复制命令|一致|官方Table，PID/CPU/内存/命令列排序，PID/用户/命令过滤，选行填PID，复制命令；信号仍确认|apps/desktop/src/renderer/components/SystemInfoPanel.tsx;NetworkMonitorPane.tsx|
|UI-071|监控/诊断|网络连接官方表格、PID/端口/进程搜索|有差异|官方Table及PID/端口/IP/进程搜索已做；v1监听器排序/详情还不全|apps/desktop/src/renderer/components/SystemInfoPanel.tsx;NetworkMonitorPane.tsx|
|UI-072|监控/诊断|进程信号发送前确认|一致|已有显式checkbox|apps/desktop/src/renderer/components/SystemInfoPanel.tsx;NetworkMonitorPane.tsx|
|UI-073|监控/诊断|进程详情、网卡选择、历史曲线|缺失|数据模型/采样待扩展|apps/desktop/src/renderer/components/SystemInfoPanel.tsx;NetworkMonitorPane.tsx|
|UI-074|监控/诊断|Ping入口与计时统计|有差异|pro-bing v0.9.1实现和UI；本box ping_group_range=1 0且无CAP_NET_RAW，真实收包测试SKIP，权限错误明确展示|apps/desktop/src/renderer/components/SystemInfoPanel.tsx;NetworkMonitorPane.tsx|
|UI-075|监控/诊断|traceroute路径设置、运行/停止/输出|有差异|可选用户nexttrace subprocess，IP-only/30s/64KiB/取消，禁GeoIP/RDNS/地图参数；仅fake CLI集成，无真CLI|apps/desktop/src/renderer/components/SystemInfoPanel.tsx;NetworkMonitorPane.tsx|
|UI-076|监控/诊断|无会话/采样首帧/权限错误状态|有差异|明确无假数据，但文案和图表不同|apps/desktop/src/renderer/components/SystemInfoPanel.tsx;NetworkMonitorPane.tsx|
|UI-077|设置/导入/云/Agent|独立设置页、分类导航、持久化和恢复默认|有差异|本地preferences_v2持久化、分类/保存/未保存恢复默认；未支持旧项集中显示原因，不保存假开关|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-078|设置/导入/云/Agent|外观system/light/dark持久化|一致|默认跟系统；顶部和设置页保存后重启恢复|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-079|设置/导入/云/Agent|字体fontFamily/fontSize14/lineHeight1.2|有差异|字号/行高默认同v1，字体默认Linux CJK/系统等宽而非v1 CSS字体栈，保留上轮中文修复|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-080|设置/导入/云/Agent|终端预设default/Dracula/Solarized/Gruvbox/Nord与前背景色|有差异|所有配色入口可用；默认跟随新UI亮暗色而非v1纯黑|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-081|设置/导入/云/Agent|localShell preset/system/customPath|有差异|系统默认/自定义单个可执行文件已接官方Command；未复制跨平台preset菜单和文件picker|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-082|设置/导入/云/Agent|OSC剪贴板write=true/read=false、通知true、标题true、链接确认true|有差异|入口已齐；v2写/读默认均拒绝，读可选择逐次询问；应用内通知不等于系统通知|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-083|设置/导入/云/Agent|shellIntegration auto/manual/off|缺失|v2仅手工复制安装命令|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-084|设置/导入/云/Agent|上传默认~/下载默认~/Downloads|有差异|持久化；文件页按钮显式应用，~在Platform展开，不静默覆盖用户输入|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-085|设置/导入/云/Agent|remoteEdit builtin/external与默认编辑器|有差异|可持久化默认外部程序，仍显式点启动；自动模式未做|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-086|设置/导入/云/Agent|全局keepalive enabled=true/interval15，连接可覆盖|一致|新建默认继承enabled/15s，覆盖优先；仅dial解析，不冻结Reconnect继承策略，6项回归|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-087|设置/导入/云/Agent|关闭前确认true、左右/底部默认折叠false|缺失|当前固定确认/固定展开|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-088|设置/导入/云/Agent|APP背景图/透明度60(30..80)/透出/WebGL|有差异|不是mygo原生渲染同构项，未做|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-089|设置/导入/云/Agent|连接管理弹窗1180x760、列宽240/340|有差异|v2主页面+Split，不是弹窗|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-090|设置/导入/云/Agent|nexttracePath与PoW provider|缺失|无设置|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-091|设置/导入/云/Agent|Agent enabled=false/port41777/timeout60/foreground/approval auto/blacklist|有差异|v2 token+严格批准，保留更强安全边界|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-092|设置/导入/云/Agent|v1明文/加密nextshell-connections文件导入导出|一致|TS向量双向验证|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-093|设置/导入/云/Agent|FinalShell解密与预览导入|一致|仅本项目v1实现为spec|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-094|设置/导入/云/Agent|v1数据库/旧device-key/keytar升级迁移|按既定决策不做|仅用户手工文件导入|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-095|设置/导入/云/Agent|v1云v3同工作区/认证材料推导加密key|按既定决策不做|v2独立version2协议|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-096|设置/导入/云/Agent|旧XOR导入路径|按既定决策不做|明确拒绝|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-097|设置/导入/云/Agent|新云HTTP登录/推拉/CAS冲突/后台调度|一致|仅参考服务端集成；真实服务器未验证|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-098|设置/导入/云/Agent|Agent MCP token认证/轮换/暂停/批准|一致|既有回归|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-099|设置/导入/云/Agent|Agent harness/skill配置复制与foreground接管标记|有差异|v2 background核心，缺部分UX|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-100|设置/导入/云/Agent|托盘、自动更新、多窗口发布细节|按既定决策不做|alex阶段4负责，本轮不动|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|
|UI-101|设置/导入/云/Agent|关于/调试日志/renderer错误报告|有差异|Go无renderer IPC，同类日志与about入口不足|apps/desktop/src/renderer/components/SettingsCenterModal.tsx;settings-center;packages/core/src/index.ts:771|

## 118 IPC逐项能力映射

118为 channels.ts:2-128 实际常量数，不把注释算通道。事件/invoke均计入；Go不重建Electron IPC。

|名称|1.x通道|状态|Go对应或缺口|
|---|---|---|---|
|ConnectionList|nextshell:connection:list|一致|desktop Snapshot/SaveConnection + storage；端口/认证/秘密语义测试|
|ConnectionUpsert|nextshell:connection:upsert|一致|desktop Snapshot/SaveConnection + storage；端口/认证/秘密语义测试|
|ConnectionBatchAuthUpdate|nextshell:connection:batch-auth:update|有差异|v1 connection-service.ts:378-415逐条继续并汇总失败；v2 appui.batchBind首错停止，尚无完整错误汇总|
|ConnectionRemove|nextshell:connection:remove|有差异|SaveConnection Deleted=true；软删除，v2不同tombstone/回收策略|
|SessionOpen|nextshell:session:open|一致|desktop Connect/Send/CloseSession + 官方terminal Conn读写/resize；真实本地SSH/PTY探针|
|SessionWrite|nextshell:session:write|一致|desktop Connect/Send/CloseSession + 官方terminal Conn读写/resize；真实本地SSH/PTY探针|
|SessionResize|nextshell:session:resize|一致|desktop Connect/Send/CloseSession + 官方terminal Conn读写/resize；真实本地SSH/PTY探针|
|SessionClose|nextshell:session:close|一致|desktop Connect/Send/CloseSession + 官方terminal Conn读写/resize；真实本地SSH/PTY探针|
|SessionData|nextshell:session:data|一致|desktop Connect/Send/CloseSession + 官方terminal Conn读写/resize；真实本地SSH/PTY探针|
|SessionStatus|nextshell:session:status|有差异|Terminal Done/Exited/Poll事件；非v1 SessionDescriptor状态机逐字兼容|
|SessionGetHomeDir|nextshell:session:get-home-dir|缺失|SessionDir只有OSC7当前目录，不等于独立remote home查询接口|
|StreamDeliveryAck|nextshell:stream:delivery:ack|有差异|官方terminal自身流控，无Electron renderer delivery ACK协议|
|MonitorSystemInfoSnapshot|nextshell:monitor:system-info:snapshot|一致|desktop RefreshMonitor + monitor.Probe；Linux快照|
|MonitorSystemStart|nextshell:monitor:system:start|有差异|desktop WatchMonitor统一2s快照，不是v1三类独立订阅；表格过滤已做|
|MonitorSystemStop|nextshell:monitor:system:stop|有差异|desktop WatchMonitor统一2s快照，不是v1三类独立订阅；表格过滤已做|
|MonitorSystemSelectInterface|nextshell:monitor:system:select-interface|缺失|缺独立网卡选择和逐PID扩展详情入口|
|MonitorSystemData|nextshell:monitor:system:data|有差异|desktop WatchMonitor统一2s快照，不是v1三类独立订阅；表格过滤已做|
|MonitorProcessStart|nextshell:monitor:process:start|有差异|desktop WatchMonitor统一2s快照，不是v1三类独立订阅；表格过滤已做|
|MonitorProcessStop|nextshell:monitor:process:stop|有差异|desktop WatchMonitor统一2s快照，不是v1三类独立订阅；表格过滤已做|
|MonitorProcessData|nextshell:monitor:process:data|有差异|desktop WatchMonitor统一2s快照，不是v1三类独立订阅；表格过滤已做|
|MonitorProcessDetail|nextshell:monitor:process:detail|缺失|缺独立网卡选择和逐PID扩展详情入口|
|MonitorProcessKill|nextshell:monitor:process:kill|一致|desktop SignalProcess；信号白名单/PID校验，UI确认|
|MonitorNetworkStart|nextshell:monitor:network:start|有差异|desktop WatchMonitor统一2s快照，不是v1三类独立订阅；表格过滤已做|
|MonitorNetworkStop|nextshell:monitor:network:stop|有差异|desktop WatchMonitor统一2s快照，不是v1三类独立订阅；表格过滤已做|
|MonitorNetworkData|nextshell:monitor:network:data|有差异|desktop WatchMonitor统一2s快照，不是v1三类独立订阅；表格过滤已做|
|MonitorNetworkConnections|nextshell:monitor:network:connections|有差异|desktop WatchMonitor统一2s快照，不是v1三类独立订阅；表格过滤已做|
|CommandExec|nextshell:command:exec|有差异|sshclient.Exec供Agent/监控后台使用，UI ExecuteCommand向终端发送；并非v1按connectionId一次性exec API|
|SftpList|nextshell:sftp:list|一致|desktop ListFiles + pkg/sftp RealPath/List|
|SftpUpload|nextshell:sftp:upload|一致|QueueTransfer upload/download；安全覆盖、上下文取消、进度|
|SftpDownload|nextshell:sftp:download|一致|QueueTransfer upload/download；安全覆盖、上下文取消、进度|
|SftpUploadPacked|nextshell:sftp:upload-packed|一致|transfer UploadPacked/DownloadPacked，本地Go tar与远端exec；真实SSH测试|
|SftpDownloadPacked|nextshell:sftp:download-packed|一致|transfer UploadPacked/DownloadPacked，本地Go tar与远端exec；真实SSH测试|
|SftpTransferPacked|nextshell:sftp:transfer-packed|缺失|跨服务器普通文件已有sftpfs.Cross；跨服务器目录打包尚缺|
|SftpMkdir|nextshell:sftp:mkdir|一致|FileAction mkdir/rename，确认及输入校验|
|SftpRename|nextshell:sftp:rename|一致|FileAction mkdir/rename，确认及输入校验|
|SftpDelete|nextshell:sftp:delete|有差异|FileAction remove仅普通文件/链接/空目录；不复制v1递归删目录|
|CommandHistoryList|nextshell:command-history:list|一致|storage History/SaveHistory + UI历史管理，本地明文有提示|
|CommandHistoryPush|nextshell:command-history:push|一致|storage History/SaveHistory + UI历史管理，本地明文有提示|
|CommandHistoryRemove|nextshell:command-history:remove|一致|storage History/SaveHistory + UI历史管理，本地明文有提示|
|CommandHistoryClear|nextshell:command-history:clear|一致|storage History/SaveHistory + UI历史管理，本地明文有提示|
|SavedCommandListScoped|nextshell:saved-command:list-scoped|有差异|store.Commands统一列表；UI分组过滤不是v1多workspace tabs|
|SavedCommandUpsert|nextshell:saved-command:upsert|一致|storage SaveCommand/Delete，具体命令名+不可恢复确认|
|SavedCommandRemove|nextshell:saved-command:remove|一致|storage SaveCommand/Delete，具体命令名+不可恢复确认|
|SettingsGet|nextshell:settings:get|有差异|model Preferences + storage preferences_v2 + desktop缓存；支持的子集，不导入旧settings|
|SettingsUpdate|nextshell:settings:update|有差异|model Preferences + storage preferences_v2 + desktop缓存；支持的子集，不导入旧settings|
|DialogOpenFiles|nextshell:dialog:open-files|缺失|当前使用路径输入与官方拖入；未接native文件/目录选择器|
|DialogOpenDirectory|nextshell:dialog:open-directory|缺失|当前使用路径输入与官方拖入；未接native文件/目录选择器|
|DialogOpenPath|nextshell:dialog:open-path|有差异|新OpenURL只允许http/https并确认；本地文件打开/在文件夹显示仍缺|
|SftpEditOpen|nextshell:sftp:edit:open|一致|OpenRemote/SaveRemote + 官方editor，UTF8/8MiB/远端hash冲突|
|SftpEditStop|nextshell:sftp:edit:stop|有差异|内置单文档与外部edit任务生命周期已做，无完整v1多文档列表/stop-all UI|
|SftpEditStopAll|nextshell:sftp:edit:stop-all|有差异|内置单文档与外部edit任务生命周期已做，无完整v1多文档列表/stop-all UI|
|SftpEditList|nextshell:sftp:edit:list|有差异|内置单文档与外部edit任务生命周期已做，无完整v1多文档列表/stop-all UI|
|SftpEditStatus|nextshell:sftp:edit:status|有差异|内置单文档与外部edit任务生命周期已做，无完整v1多文档列表/stop-all UI|
|SftpEditReadFile|nextshell:sftp:edit:read-file|一致|OpenRemote/SaveRemote + 官方editor，UTF8/8MiB/远端hash冲突|
|SftpEditWriteFile|nextshell:sftp:edit:write-file|一致|OpenRemote/SaveRemote + 官方editor，UTF8/8MiB/远端hash冲突|
|SftpTransferStatus|nextshell:sftp:transfer:status|一致|transfer Queue List/Cancel + Invalidate；新增ClearCompleted/CancelAll不移除失败|
|SftpTransferCancel|nextshell:sftp:transfer:cancel|一致|transfer Queue List/Cancel + Invalidate；新增ClearCompleted/CancelAll不移除失败|
|CloudSyncWorkspaceList|nextshell:cloud-sync:workspace:list|按既定决策不做|不接v1云v3协议/工作区/邀请token。v2有CloudOverview/Connect/Sync/Background，version2密钥分离；v2工作区删除/改名/邀请文件也未齐|
|CloudSyncWorkspaceAdd|nextshell:cloud-sync:workspace:add|按既定决策不做|不接v1云v3协议/工作区/邀请token。v2有CloudOverview/Connect/Sync/Background，version2密钥分离；v2工作区删除/改名/邀请文件也未齐|
|CloudSyncWorkspaceUpdate|nextshell:cloud-sync:workspace:update|按既定决策不做|不接v1云v3协议/工作区/邀请token。v2有CloudOverview/Connect/Sync/Background，version2密钥分离；v2工作区删除/改名/邀请文件也未齐|
|CloudSyncWorkspaceRemove|nextshell:cloud-sync:workspace:remove|按既定决策不做|不接v1云v3协议/工作区/邀请token。v2有CloudOverview/Connect/Sync/Background，version2密钥分离；v2工作区删除/改名/邀请文件也未齐|
|CloudSyncWorkspaceExportToken|nextshell:cloud-sync:workspace:export-token|按既定决策不做|不接v1云v3协议/工作区/邀请token。v2有CloudOverview/Connect/Sync/Background，version2密钥分离；v2工作区删除/改名/邀请文件也未齐|
|CloudSyncWorkspaceParseToken|nextshell:cloud-sync:workspace:parse-token|按既定决策不做|不接v1云v3协议/工作区/邀请token。v2有CloudOverview/Connect/Sync/Background，version2密钥分离；v2工作区删除/改名/邀请文件也未齐|
|CloudSyncStatus|nextshell:cloud-sync:status|按既定决策不做|不接v1云v3协议/工作区/邀请token。v2有CloudOverview/Connect/Sync/Background，version2密钥分离；v2工作区删除/改名/邀请文件也未齐|
|CloudSyncSyncNow|nextshell:cloud-sync:sync-now|按既定决策不做|不接v1云v3协议/工作区/邀请token。v2有CloudOverview/Connect/Sync/Background，version2密钥分离；v2工作区删除/改名/邀请文件也未齐|
|CloudSyncTestConnection|nextshell:cloud-sync:test-connection|按既定决策不做|不接v1云v3协议/工作区/邀请token。v2有CloudOverview/Connect/Sync/Background，version2密钥分离；v2工作区删除/改名/邀请文件也未齐|
|CloudSyncStatusEvent|nextshell:cloud-sync:status:event|按既定决策不做|不接v1云v3协议/工作区/邀请token。v2有CloudOverview/Connect/Sync/Background，version2密钥分离；v2工作区删除/改名/邀请文件也未齐|
|CloudSyncAppliedEvent|nextshell:cloud-sync:applied:event|按既定决策不做|不接v1云v3协议/工作区/邀请token。v2有CloudOverview/Connect/Sync/Background，version2密钥分离；v2工作区删除/改名/邀请文件也未齐|
|AgentStatus|nextshell:agent:status|一致|AgentOverview/Endpoint/Halt/Respond，gateway pending/activity与MCP token认证；本地参考测试|
|AgentEnable|nextshell:agent:enable|一致|AgentOverview/Endpoint/Halt/Respond，gateway pending/activity与MCP token认证；本地参考测试|
|AgentDisable|nextshell:agent:disable|一致|AgentOverview/Endpoint/Halt/Respond，gateway pending/activity与MCP token认证；本地参考测试|
|AgentActivityEvent|nextshell:agent:activity:event|一致|AgentOverview/Endpoint/Halt/Respond，gateway pending/activity与MCP token认证；本地参考测试|
|AgentSetHalted|nextshell:agent:set-halted|一致|AgentOverview/Endpoint/Halt/Respond，gateway pending/activity与MCP token认证；本地参考测试|
|AgentSessionControlEvent|nextshell:agent:session:control:event|缺失|v1前台独占接管标记/回收流程未完整移植，v2后台模式为主|
|AgentSessionFocusEvent|nextshell:agent:session:focus:event|有差异|事件FocusID可切现有会话；不复刻完整v1前台控制状态|
|AgentOpenRequestEvent|nextshell:agent:open:request|一致|AgentOverview/Endpoint/Halt/Respond，gateway pending/activity与MCP token认证；本地参考测试|
|AgentOpenRespond|nextshell:agent:open:respond|一致|AgentOverview/Endpoint/Halt/Respond，gateway pending/activity与MCP token认证；本地参考测试|
|ConnectionRevealPassword|nextshell:connection:reveal-password|缺失|v2 UI不显示已保存秘密；需导出文件时用户明确操作|
|ConnectionFolderList|nextshell:connection-folder:list|一致|storage Folders/SaveFolder；父目录选择；新增树菜单|
|ConnectionFolderCreate|nextshell:connection-folder:create|一致|storage Folders/SaveFolder；父目录选择；新增树菜单|
|ConnectionFolderRename|nextshell:connection-folder:rename|一致|storage Folders/SaveFolder；父目录选择；新增树菜单|
|ConnectionFolderMove|nextshell:connection-folder:move|一致|storage Folders/SaveFolder；父目录选择；新增树菜单|
|ConnectionFolderReorder|nextshell:connection-folder:reorder|缺失|无持久化sortOrder/拖动目录排序|
|ConnectionFolderRemove|nextshell:connection-folder:remove|有差异|Delete只允许空且无引用目录，守住资源关系不递归删除|
|SshKeyList|nextshell:ssh-key:list|一致|Snapshot/SaveKey/Delete/GenerateKey，x/crypto inspect/generate|
|SshKeyUpsert|nextshell:ssh-key:upsert|一致|Snapshot/SaveKey/Delete/GenerateKey，x/crypto inspect/generate|
|SshKeyRemove|nextshell:ssh-key:remove|一致|Snapshot/SaveKey/Delete/GenerateKey，x/crypto inspect/generate|
|SshKeyGenerate|nextshell:ssh-key:generate|一致|Snapshot/SaveKey/Delete/GenerateKey，x/crypto inspect/generate|
|SshKeyUsage|nextshell:ssh-key:usage|缺失|没有完整用途统计入口|
|ProxyList|nextshell:proxy:list|一致|Snapshot/SaveProxy/Delete，凭据加密与引用检查|
|ProxyUpsert|nextshell:proxy:upsert|一致|Snapshot/SaveProxy/Delete，凭据加密与引用检查|
|ProxyRemove|nextshell:proxy:remove|一致|Snapshot/SaveProxy/Delete，凭据加密与引用检查|
|ConnectionExport|nextshell:connection:export|一致|ExportConnections ids，v1 plain/encrypted文件，TS向量|
|ConnectionExportBatch|nextshell:connection:export:batch|一致|ExportConnections ids，v1 plain/encrypted文件，TS向量|
|ConnectionImportPreview|nextshell:connection:import:preview|一致|PreviewImport/CommitImport token；目录递归、保护symlink/size/count、preview确认、3冲突策略|
|ConnectionImportFinalShellPreview|nextshell:connection:import:finalshell:preview|一致|PreviewImport/CommitImport token；目录递归、保护symlink/size/count、preview确认、3冲突策略|
|ConnectionImportDirectoryPreview|nextshell:connection:import:directory:preview|一致|PreviewImport/CommitImport token；目录递归、保护symlink/size/count、preview确认、3冲突策略|
|ConnectionImportExecute|nextshell:connection:import:execute|一致|PreviewImport/CommitImport token；目录递归、保护symlink/size/count、preview确认、3冲突策略|
|UpdateCheck|nextshell:update:check|按既定决策不做|alex阶段4负责，不改自动更新/托盘/多窗口|
|SecurityDeviceKeyNoticeGet|nextshell:security:device-key-notice:get|按既定决策不做|旧keychain/device-key降级迁移提示不移植；v2自己新密钥留DB|
|SecurityDeviceKeyNoticeAcknowledge|nextshell:security:device-key-notice:acknowledge|按既定决策不做|旧keychain/device-key降级迁移提示不移植；v2自己新密钥留DB|
|SecurityDeviceKeyNoticeEvent|nextshell:security:device-key-notice:event|按既定决策不做|旧keychain/device-key降级迁移提示不移植；v2自己新密钥留DB|
|DebugLogEnable|nextshell:debug:log:enable|缺失|没有v1动态详细日志开关；release inspector禁用，开发入口既有env|
|DebugLogDisable|nextshell:debug:log:disable|缺失|没有v1动态详细日志开关；release inspector禁用，开发入口既有env|
|DebugLogEvent|nextshell:debug:log:event|有差异|活动日志/Go错误，不存在React renderer全局错误收集；不重建Electron IPC|
|DebugRendererError|nextshell:debug:renderer-error|有差异|活动日志/Go错误，不存在React renderer全局错误收集；不重建Electron IPC|
|Ping|nextshell:ping|有差异|diagnostics.Ping pro-bing v0.9.1，IP-only；本box非特权ICMP被内核拒绝，真实收包SKIP|
|TracerouteRun|nextshell:traceroute:run|有差异|Diagnose/CancelDiagnostic + bounded optional nexttrace CLI；目前完成后整体输出，非v1实时逐hop事件；仅fake CLI测试|
|TracerouteStop|nextshell:traceroute:stop|有差异|Diagnose/CancelDiagnostic + bounded optional nexttrace CLI；目前完成后整体输出，非v1实时逐hop事件；仅fake CLI测试|
|TracerouteData|nextshell:traceroute:data|有差异|Diagnose/CancelDiagnostic + bounded optional nexttrace CLI；目前完成后整体输出，非v1实时逐hop事件；仅fake CLI测试|
|TerminalNotification|nextshell:terminal:notification|有差异|官方OnBell/OnNotify应用内消息；不等于系统桌面通知|
|TerminalProgress|nextshell:terminal:progress|缺失|未接OS任务栏进度与系统通知action|
|TerminalNotificationAction|nextshell:terminal:notification-action|缺失|未接OS任务栏进度与系统通知action|
|ResourceCopyConnection|nextshell:resource:copy-connection|一致|DuplicateConnection/CloudCopy；复制新UUID与加密secret，不复制旧v1 scope协议|
|RecycleBinList|nextshell:recycle-bin:list|一致|Connections Deleted字段列表/恢复；永久删除资源+secret，保留sync tombstone|
|RecycleBinRestore|nextshell:recycle-bin:restore|一致|Connections Deleted字段列表/恢复；永久删除资源+secret，保留sync tombstone|
|RecycleBinPurge|nextshell:recycle-bin:purge|一致|Connections Deleted字段列表/恢复；永久删除资源+secret，保留sync tombstone|
|RecycleBinClear|nextshell:recycle-bin:clear|缺失|缺一键清空回收站|

## 设置默认值逐项表

见 SETTINGS.md（所有 packages/core/src/index.ts:771-833 默认字段逐项列出）。设置未实现项在新版设置页有说明入口，不保存不起作用的伪开关。

## 本轮边界与待补

- 当前交互表不是全部1137候选逐支验收；复杂scope UI、完整错误文案本地化、原生文件选择器、SFTP多选剪切粘贴、跨服目录打包、多文档编辑/全局脏状态、监控历史曲线/网卡详情、命令组CRUD及参数历史、CtrlTab MRU均留缺口。
- 工具只有官方离屏UI.Tester；没有原生桌面点击工具。截图明确标离屏，另留真实桌面只读截图供人工复验。
- 保留前次已知原生输入延迟/掉字问题的UNVERIFIED状态；本轮不声称修复原生队列。
- Cloud仍仅新version2协议+参考服务端本地测试；不接真实服务；无push/PR；不处理alex阶段4。
