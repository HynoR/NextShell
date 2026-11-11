# NextShell v2 原生 UI 美化报告

2026-11-11，Asia/Shanghai（UTC+8）。仅本地 v2 提交，无 push/PR，无外部真实服务访问。

## 结论与提交

已把“整排灰按钮”工作空间改为主题化侧栏 + 图标活动栏 + 多文档标签 + 分层工具栏/统一表单卡片。亮/暗均有实现，默认跟随系统；release 已启动并保持运行。没有更换框架、没有修改本轮之前的 mygo fork、没有读其他 SSH 客户端代码。

- `ded6cad`：mygo v0.8.3 官方能力调研。
- `8059b5b`：UI 实现、MIT SVG、文件表格及回归测试。
- `02e1c18`：外观/导航说明与许可出处。
- 本报告另作本地文档提交，hash 见最终回报。

## 调研事实与出处

以下均为**官方 v0.8.3**，源码完整缓存根 `/home/box/go/pkg/mod/github.com/egoist/mygo@v0.8.3/`；仓库基址 https://github.com/egoist/mygo/blob/v0.8.3/ 。详见 `docs/v2/UI-RESEARCH.md`。

| 能力 | 已核实 API / 限制 | 官方路径 |
|---|---|---|
| 主题、暗色、token、强调色 | Theme、LightTheme/DarkTheme、Context.SetTheme/Themed、Preferences；默认系统外观/文字缩放/高对比 | ui/theme.go；docs/ui/styling.md |
| 圆角/阴影/间距/字体 | Radius/Shadow/InsetShadow/Padding/Gap/FontSize/FontWeight/Gradient；无 CSS，状态由 Go 视图决定 | ui/element.go；ui/api_gen.go；docs/ui/styling.md |
| 图标 | ui.Icon + SVG/currentColor；不是自带跨平台图标库；SF Symbols 只在 macOS 可用 | ui/svg.go；docs/ui/icon.md；docs/plugins/sfsymbols.md |
| 侧栏、列表、表格、树 | SidebarItem/Section 原生选中/hover/折叠/键盘语义；List/Table/Outline 官方虚拟化；ListState.RoundRows | ui/sidebar.go、list.go、table.go、outline.go；docs/ui 同名文档 |
| 标签、分割 | Tabs 下划线；TabsBase 可组合自定义外观、关闭/拖动；Split/SplitReset 官方调宽 | ui/base.go、tabs.go、split.go；docs/ui/tabs.md、split.md |
| 表单/对话框 | Form/Field、LabelledBy、AlertDialog；此次保留既有 field helper 和统一 token，删除确认仍用官方 AlertDialog | ui/form.go；docs/ui/form.md、alert-dialog.md |
| 窗口与材质 | TitleBarHidden/TitleBar/DragWindow 保留原生控制安全区；Vibrancy 无 Linux 窗口背景效果；glass 插件可画同窗口内容材质，不等同系统窗口毛玻璃 | window.go；docs/ui/windows.md；docs/plugins/glass.md |
| 官方优秀示例 | notes 的侧栏/轻工具栏、code-editor 的分区、gallery 的组件与 SVG/Tabs/Split、vibrancy 的平台降级 | examples/notes/main.go:92；examples/code-editor/main.go:65；examples/gallery/main.go:297/500/859；examples/vibrancy/main.go |

## 实际改动

1. 左侧使用官方 Sidebar/SidebarSection/SidebarItem：连接图标、分组折叠、hover/选中、编辑小图标与搜索。官方 Split 支持拖动分隔，双击恢复260DIP。
2. 右侧改64DIP图标导航，保留所有14个页面入口，保留原中文语义 label，增加 tooltip 和强调选中态。
3. 顶部品牌/快连/外观工具栏、独立48DIP标签栏；标签基于官方 TabsBase，保留关闭、拖动、重命名、OSC 手动优先与重连规则。会话工具栏减少灰按钮。
4. 空状态增加图标、主次文字与明确入口。所有页面共享 Theme 的颜色；页面/卡片/字段的间距、圆角与标签宽度集中在 `v2/internal/appui/design.go`，保存类按钮有统一主操作色。编辑器继续用官方 editor。
5. 文件页改官方虚拟化 Table，不再把文件名画成长灰按钮；支持行选择、Enter/双击、名称按钮单击打开目录、编辑动作，保留拖放、传输和远端操作。
6. 青绿强调色、深海蓝暗色/灰白亮色；默认跟随系统，可在顶部选浅色/深色。terminal 使用官方 Terminal.SetTheme 接入两套16色 ANSI 配色，没有改字体、终端输入队列或渲染器。
7. 图标为本项目原创精简 SVG，**MIT**，许可全文 `v2/internal/appui/ICONS-LICENSE.txt`；由官方 ui.Icon 绘制，不引入第三方图标字体/社区组件库。

## 验证（真实结果）

最终源代码 build、vet、普通 test、race、nextshell_devtools 的 cmd 测试、mygo_noinspector 构建均 exit 0。

| 每次全量测试 | 顶层总数/通过 | 通过 runs（含子测试） | leaf 总数/通过 | 失败 | 跳过 |
|---|---:|---:|---:|---:|---:|
| go test -json -count=1 ./... | 163/162 | 397 | 360/359 | 0 | 1 |
| go test -race -json -count=1 ./... | 163/162 | 397 | 360/359 | 0 | 1 |

18个有测试包通过。唯一 skip 为 zsh hook（box 无 zsh），不是通过。新增主题/系统切换、双主题导航、侧栏选择/编辑/折叠、SFTP表格选择/编辑/目录打开测试；既有快捷键、重命名、删除、OSC、token 等回归仍全过。原树测试仅同步新的正式标签（去掉模拟缩进/箭头字符），没有删测试。真实 localhost SSH/SFTP/代理/传输/云参考服务/Agent/MCP 等探针 **21/21**；profile 重开密码/密钥登录验证通过。证据为本目录 test.json/race.json/counts.jsonl/live-probe.log/reopen.log。

## 截图与运行实例

- 根目录：`/workspace/NextShell-notes/ui-refresh/`。
- `SCREENSHOTS.md`：全部路径索引；`before-{light,dark}-{empty,ssh,files,connection}.png`、`after-...`，共16张原图，8张 `compare-...` 左旧右新对比图，每页1440×1000。
- **这16张业务页截图为官方 ui.Tester 离屏渲染**，接的是实际本机 SSH/SFTP 数据，不是原生桌面截图。没有可用的原生桌面导航驱动，因此没有假装点开桌面所有页面。
- `desktop-before.png` / `desktop-after.png` 为真实 DISPLAY=:17 空状态只读截屏。
- release：`/workspace/NextShell-notes/linux-test/bin/nextshell`；构建 `go build -trimpath -tags mygo_noinspector`，代码8059b5b，构建时HEAD为文档提交02e1c18、树干净。
- 已用既有 launch-gui.py 启动，DISPLAY=:17，PID **405028**；启动记录 2026-11-11T13:24:03.238839+08:00（Asia/Shanghai）。原 profile 保留，未重置测试连接/人工数据。
- 既有 stop-gui.py 的 SIGTERM 本轮超时；核实 PID、exe 和 profile 后，仅对自己这个旧实例通过 pidfd SIGKILL，再重启。后续重开验证通过；不能据此宣称找到了旧实例退出超时的根因。

## 默认取舍、限制与未验证

- 外观选择仅本窗口，重启恢复跟随系统，不新增业务schema/云同步字段。
- 不用玻璃、大阴影和持续动画，以免增加仍待定位的原生输入延迟；Linux 无原生窗口 Vibrancy，但窗口内部 glass 并非框架做不到，只是本轮没有选用。
- 默认强调色使用统一品牌青绿；系统高对比与文字缩放走官方偏好。没有复制第三方 SSH 客户端界面代码。
- 未做 Windows/macOS 真机验收、原生系统暗色通知/中文 IME 验收；亮暗与系统变化在官方测试器内通过。
- 之前报告的原生输入卡顿/丢字/乱序仍未定位；本轮没有宣称通过美化修复它。跨应用原生剪贴板仍需人工复验；OSC133继续等上游。
- 表格列/导航均有可访问标签，但本轮没有屏幕阅读器真机测试；图标活动栏可滚动。建议人工重点点侧栏折叠/选择、全部导航、标签拖动/复制粘贴、文件行/编辑/拖放、外观切换及小窗口布局。
