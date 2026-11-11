# 原生工作空间外观

- 顶部“外观”可选跟随系统、浅色、深色。启动默认跟随系统，本轮手工选项只作用当前窗口，不写入业务数据库。系统变化、文字缩放和高对比偏好通过官方 Theme/Preferences 接入。
- 左侧官方 Sidebar 的分组可折叠，行有图标/hover/选中态；点击连接名连接，右侧小编辑图标进入设置。中间官方 Split 分隔线可拖动调宽，双击恢复260DIP。
- 右边图标活动栏保留全部页面入口，各图标有原中文 label 与 tooltip。
- 会话标签基于官方 TabsBase；保留关闭、拖动、手动名称/OSC 优先规则，工具栏弱化重复灰色按钮。
- 文件页使用官方虚拟化 Table：行选择/Enter/双击，名称按钮单击进目录，编辑图标打开文件；保留文件拖放与传输/远端操作。表格自身可滚动，不是手写列表。
- 亮/暗配色及间距 token：v2/internal/appui/design.go。终端两套16色 ANSI palette 通过官方 Terminal.SetTheme 应用，不更改字体、终端渲染器或编辑器。
- 图标为本项目原创精简 SVG，通过官方 ui.Icon 渲染，MIT 许可见 v2/internal/appui/ICONS-LICENSE.txt；不依赖系统图标字体或社区组件库。
- 不启用毛玻璃/持续动画/重阴影。Linux 无原生窗口背景 Vibrancy；macOS/Windows 真机效果未验证。release 仍用 mygo_noinspector，不重新开放调试面板。
