# NextShell 2 更新日志

## 2.0.0-rc1

NextShell 2 的第一个候选版本(预发布)。

### 这是什么

NextShell 2 是**全新的产品**,用 Go 和原生界面重写(不是 Electron,也不是网页),不是 1.x 的升级版:

- 不读取、不迁移 1.x 的数据库、设备密钥和钥匙串。
- 1.x 用户:先在 1.x 里导出连接文件,再到 2 的「设置 → 导入导出」(或首次启动的欢迎页)导入。
- 2 使用独立的数据目录,不会动 1.x 的数据;预发布版不会出现在 1.x 的更新提示里。

### 亮点

- **原生界面**:不是网页套壳,深浅色可跟随系统。
- **终端为中心**:终端始终在场;文件、监控、命令片段是右侧面板,不会盖住终端。主机栏、多标签,`Cmd/Ctrl+K` 命令面板一个入口找到主机、标签、片段和各种操作。
- **SFTP 文件面板**:面包屑、多选、右键菜单、拖入上传、目录自动打包传输、状态栏传输列表。
- **服务器监控**:CPU、内存、网络、磁盘,进程和网络连接。
- **命令片段**:搜索、一键发送、带参数的模板。
- **远程编辑**:远程文件在独立窗口里编辑、保存回服务器,也可交给外部编辑器。
- **给 AI Agent 用的 MCP 端点**:默认关闭;开启后需要令牌,应用把端口和令牌写入用户配置目录下的 `mcp.json`,配套的 MCP 桥自动读取,不用手工配置。桥不在 npm 上,随本版本以 `nextshell-mcp-0.2.0.tgz` 发布(需要 Node.js 24+)。
- **三级命令策略**:禁止(永不执行)/ 风险(始终手动批准)/ 其余(可自动批准,有时限,重启即关闭)。命令输出自动脱敏。这是防误操作的护栏,不是安全沙箱,只在信任当前 Agent 任务时开启自动批准。
- **不打扰**:每次运行最多提示一次,之后 Agent 在后台标签里干活,不抢焦点、不切标签;你正在输入时它不会往里写。
- **主机指纹**:首次连接需确认,指纹变化直接拒绝并提示。

### 接入 Claude Code

装 Claude Code 插件(`/plugin marketplace add HynoR/NextShell`,`/plugin install nextshell`),或不装插件直接添加:

```sh
claude mcp add nextshell -- npx --allow-remote=root -y https://github.com/HynoR/NextShell/releases/download/v2.0.0-rc1/nextshell-mcp-0.2.0.tgz
```

`--allow-remote=root` 不能省:npm 12 起默认禁止从 URL 安装,这个选项只放行命令行上的这个地址。其他支持 MCP 的客户端同理,命令是 `npx --allow-remote=root -y <上面的地址>`。

### 下载

- macOS(Intel / Apple 芯片通用,12 及以上):`.dmg` 拖进「应用程序」,或 `.zip`。
- Windows(x64 / ARM64):`-setup.exe` 安装到当前用户目录,无需管理员;或 `.zip` 解压即用。
- Linux(x64):`.deb`、`.rpm`(装好后命令是 `nextshell`)、`.tar.gz`。
- `nextshell-mcp-0.2.0.tgz`:给 AI Agent 用的 MCP 桥,见上。
- `SHA256SUMS.txt` 是全部文件的校验值。

### 已知限制

- **未签名**:本版本没有 Apple 开发者签名、公证和 Windows 代码签名。
  - macOS:首次打开请右键 → 打开;或运行 `xattr -dr com.apple.quarantine /Applications/NextShell.app`。
  - Windows:SmartScreen 提示时点「更多信息 → 仍要运行」。
- Linux 需要 GTK 3(主流桌面发行版已自带)。
- 没有自动更新,请到发布页下载新版本。
- 云同步在本版本不可用。
- 暂不支持 OSC 133(Shell 提示符标记),等待上游框架开放相关事件。
- 文件面板暂不支持把文件拖出到 Finder / 资源管理器下载,请用右键「下载到…」;拖入上传可用。
- 监控和打包传输要求远端是 Linux。
- Windows 上的 SSH agent 支持见发布说明。
- 平台验证程度不同:macOS 是主要验证平台;Windows 和 Linux 是第一次发布,欢迎反馈问题。

### 反馈问题

到 [GitHub Issues](https://github.com/HynoR/NextShell/issues) 新建 issue,请写明:版本号(设置 → 关于)、操作系统、复现步骤。贴日志或截图前请去掉密码、令牌和主机地址。
