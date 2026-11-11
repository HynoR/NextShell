# mygo v0.8.3 美化能力核实

仅查本机精确版本缓存 `/home/box/go/pkg/mod/github.com/egoist/mygo@v0.8.3/` 的官方源码、docs 和 examples；没有查其他 SSH 客户端代码。下列路径均相对官方模块根；链接基址 https://github.com/egoist/mygo/blob/v0.8.3/ 。

- 主题/token：`ui/theme.go`、`docs/ui/styling.md`。LightTheme/DarkTheme；Theme 有 Background/Surface/Hover/Pressed、Accent、Selection、FontSize/Font、Radius、Spacing、Focus、Warning/Success 等。Context.Theme 默认每帧跟随系统，SetTheme 改本帧，Themed 可局部主题。Preferences 支持系统强调色/高对比/文字缩放/减少动画；不是 CSS。
- 样式：同 styling.md 与 `ui/api_gen.go`。Padding/Gap/Radius/Border/Shadow/InsetShadow、Gradient、Opacity、FontSize/Weight、Hover 状态；支持缓存/材质但不应为了装饰引入重阴影、动画和额外输入成本。
- 图标：`docs/ui/icon.md`、`ui/svg.go`；ui.Icon + ParseSVG，SVG currentColor 随主题，无内置跨平台 Lucide/Heroicons 集合。`plugins/sfsymbols` 只有 macOS 的系统符号；Linux 不可用。此轮采用小型自包含原创 MIT SVG，不加社区依赖、不依赖系统图标字体。
- Sidebar：`ui/sidebar.go`、`docs/ui/sidebar.md`。官方 SidebarItem/Section 有图标、选中、hover、键盘和折叠语义。连接树可直接用它组合，无需按钮模拟。
- List/Table/Outline：`ui/list.go`、`ui/table.go`、`ui/outline.go` 和同名 docs/ui 文档。官方虚拟化、选择、键盘、列排序/缩放、Outline 层级；Table.RoundRows 可圆角。已有文件/资源表继续使用官方实现，不手写虚拟列表。
- Tabs：`ui/tabs.go`、`ui/base.go`、`docs/ui/tabs.md`。默认下划线样式，TabsBase 提供不带外观的选择/键盘底座，可组合图标、关闭和拖动。
- Split：`ui/split.go`、`docs/ui/split.md`。官方拖动/键盘调整、SplitReset，横/纵与百分比布局。
- 表单/工具栏/对话框：`docs/ui/form.md`、`toolbar.md`、`alert-dialog.md`；Theme metrics 和统一 field helpers 可做统一对齐。
- 标题栏：`docs/ui/windows.md`、`window.go`。TitleBarHidden + Context.TitleBar + DragWindow 保留原生窗口控制安全区。Vibrancy 在 macOS/特定 Windows 可用，Linux 无窗口背后材质，必须画实色回退。`docs/plugins/glass.md` 是窗口内部渲染材质，不等于 Linux 系统窗口透明毛玻璃。此轮不启用高成本玻璃效果。
- 示例：`examples/notes/main.go:92` 使用 tint Sidebar + 内容面板、轻工具栏、次级文字；`examples/code-editor/main.go:65` 用 Sidebar 与官方 editor；`examples/gallery/main.go:297,500,859` 展示 SVG、分组侧栏、Tabs/Split 与皮肤；`examples/vibrancy/main.go` 展示系统材质降级。参考这些组合，不引入外部社区组件库。

实现方向：青绿强调色、深海蓝暗色/灰白亮色，默认跟随系统，提供窗口内外观选择；侧栏用官方 Sidebar/Section，右边紧凑图标导航，标签基于 TabsBase，官方 Split 调宽；统一主题 token 和表单间距；terminal 使用官方 SetTheme 的两套自定义 ANSI palette。
