# 1.x设置默认值逐字段核对

唯一来源：本项目3c5e5a8 `packages/core/src/index.ts:771-833`。v2入口：右侧“设置”，未实现/发布类在“兼容性与未实现”分类有明确说明。

|字段|v1默认字面值|源码行|状态|v2入口/语义与默认|
|---|---|---:|---|---|
|transfer.uploadDefaultDir|`"~"`|773|有差异|UploadDir=~，文件页按钮显式应用；不是自动替换现有路径|
|transfer.downloadDefaultDir|`"~/Downloads"`|774|有差异|DownloadDir=~/Downloads，默认下载按钮/右键可用，Platform展开~|
|remoteEdit.defaultEditorCommand|`""`|777|一致|EditorCommand空，设置页保存后作为显式外部编辑初值|
|remoteEdit.editorMode|`"builtin"`|778|缺失|内置仍默认；外部需用户显式点启动，设置页说明自动模式未实现|
|terminal.backgroundColor|`"#000000"`|781|有差异|跟随新UI外观；支持Default纯黑、自定义#RRGGBB|
|terminal.foregroundColor|`"#d8eaff"`|782|有差异|跟随新UI外观；Default预设可取回#d8eaff|
|terminal.fontSize|`14`|783|一致|FontSize默认14，10..24校验，live官方SetFont|
|terminal.lineHeight|`1.2`|784|一致|LineHeight默认1.2，1..2且拒绝NaN，live SetFont|
|terminal.fontFamily|`"JetBrains Mono, Menlo, Monaco, monospace"`|785|有差异|空=Linux Noto Sans Mono CJK SC或系统等宽，支持自定义comma字体族；不覆盖前轮中文修复|
|terminal.localShell.mode|`"preset"`|787|有差异|空路径=系统，非空=单个自定义executable；无preset列表|
|terminal.localShell.preset|`"system"`|788|有差异|官方默认shell等同system用途，无各OS shell枚举菜单|
|terminal.localShell.customPath|`""`|789|一致|LocalShell默认空，单个可执行文件，非shell命令参数|
|terminal.oscClipboardWrite|`true`|791|有差异|v2默认false，可显式开启；新会话生效，保守默认|
|terminal.oscClipboardRead|`false`|792|有差异|v2默认false，开启后ClipboardAsk逐次询问，不自动放行|
|terminal.oscNotifications|`true`|793|有差异|默认true，目前仅应用内消息；不是系统桌面通知|
|terminal.oscTitleUpdates|`true`|794|一致|默认true，live开关；手动标签名称优先|
|terminal.hyperlinkConfirm|`true`|795|一致|默认true，完整目标地址确认；额外只允许http/https且拒绝userinfo|
|terminal.shellIntegration|`"auto"`|796|缺失|手工OSC7脚本已有，无auto/manual/off偏好；OSC133延后|
|terminal.wallpaper.seeThrough|`true`|798|有差异|无xterm wallpaper等价设置，仅设置页说明，不保存伪开关|
|terminal.wallpaper.useWebgl|`false`|799|有差异|mygo原生渲染，不存在xterm WebGL开关|
|ssh.keepAliveEnabled|`true`|803|一致|默认true；dial时解析全局继承，连接可覆盖，重连重新解析|
|ssh.keepAliveIntervalSec|`15`|804|一致|默认15，5..600范围；已连会话不静默重建|
|window.appearance|`"system"`|807|一致|跟随系统/浅/深，持久化；新视觉token|
|window.minimizeToTray|`false`|808|按既定决策不做|alex阶段4负责|
|window.confirmBeforeClose|`true`|809|有差异|有会话退出确认，但无可编辑偏好和全局脏编辑守护|
|window.backgroundImagePath|`""`|810|缺失|设置页说明未实现，未接图片/文件picker|
|window.backgroundOpacity|`60`|811|缺失|无30..80材质透明度设置，未伪造|
|window.leftSidebarDefaultCollapsed|`false`|812|缺失|固定展开；已有连接组折叠不是整侧栏默认折叠偏好|
|window.bottomWorkbenchDefaultCollapsed|`false`|813|缺失|固定默认展开，可窗口内关闭，尚不保存默认|
|connectionManager.dialogWidth|`1180`|816|有差异|v2为主页面，不是弹窗；不应用旧像素宽度|
|connectionManager.dialogHeight|`760`|817|有差异|同上，窗口交由mygo布局|
|connectionManager.folderColumnWidth|`240`|818|有差异|Sidebar Split可调，不保存v1独立弹窗列宽|
|connectionManager.detailColumnWidth|`340`|819|有差异|v2不同表单布局，无独立详情列宽|
|traceroute.nexttracePath|`""`|822|一致|NexttracePath默认空=PATH查nexttrace；仅用户自己安装的程序|
|traceroute.powProvider|`"api.nxtrace.org"`|823|有差异|给CLI禁GeoIP/RDNS/地图参数，不集成PoW provider；真实CLI行为未验证|
|agent.enabled|`false`|826|有差异|默认关闭且halt；用户显式启用，未保存启用状态|
|agent.port|`41777`|827|有差异|同默认41777但固定loopback；无端口设置入口，设置页明确说明|
|agent.execTimeoutSec|`60`|828|有差异|当前每次请求timeoutSec，越界/未指定默认30秒，非60秒全局偏好|
|agent.execMode|`"foreground"`|829|按既定决策不做|仅后台；前台执行依赖OSC133安全标记，等待上游|
|agent.execApproval|`"auto"`|830|有差异|v2更严格批准/敏感文件守护，不恢复v1自动批准配置|
|agent.blacklist|`[]`|831|有差异|Agent页黑名单可编辑；无全局偏好持久化，默认拒绝危险命令|

独立设置字段共41：{'有差异': 23, '一致': 10, '缺失': 6, '按既定决策不做': 2}。不和PARITY.md交互/IPC条目相加，避免重复计数。

本轮新增非v1设置：cursor/block/bar/underline、blink、scrollback（MiB，不是行数）；使用官方插件，仅新建/重连生效。
