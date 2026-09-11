# NextShell

NextShell 是一个基于 Electron + React + TypeScript 的桌面运维客户端，使用 pnpm Workspace 管理应用与共享包。

## 功能概览

- SSH 连接管理：分组、搜索、收藏、快速连接
- 多会话终端：标签页、重连、认证重试
- SFTP 能力：远程/本地文件浏览、上传下载、打包传输、传输队列
- 远程编辑：内置编辑器与外部编辑器联动
- 运维工具：命令中心（批量执行）、Ping、Traceroute、端口转发
- 资源监控：系统、进程、网络监控（无 agent）
- 安全与数据：主密码、系统钥匙串（keytar）、本地 SQLite 存储、备份/恢复
- 资产导入：支持连接导入与 FinalShell 数据导入预览

## 技术架构

- Monorepo：pnpm Workspace
- 桌面端：Electron（main/preload/renderer）+ React + Vite + Tailwind CSS
- 类型与契约：TypeScript + Zod
- 原生依赖：`better-sqlite3`、`keytar`、`ssh2`

## 项目结构

- `apps/desktop`: Electron 应用入口
- `apps/desktop/src/main`: 主进程服务、IPC 注册、系统集成、Agent（MCP）端点
- `apps/desktop/src/preload`: 安全桥接 API（`window.nextshell`）
- `apps/desktop/src/renderer`: 前端 UI、状态管理、业务逻辑
- `packages/core`: 核心领域类型与偏好模型
- `packages/shared`: IPC channel、合同类型、跨端共享常量
- `packages/ssh`: SSH/代理相关抽象
- `packages/storage`: 存储层能力（SQLite 相关）
- `packages/security`: 安全能力（如 keytar 封装）
- `packages/terminal`: 终端会话相关抽象
- `packages/ui-kit`: 共享 UI 组件

## 环境要求

- Node.js `24.x`
- pnpm `11.15.1`（由根目录 `packageManager` 固定）
- macOS、Windows 或 Linux 图形桌面（X11 / Wayland）
- 建议安装可用的本地构建工具链，用于编译/重建原生模块

## 快速开始

```bash
pnpm run setup
pnpm run dev
```

`pnpm run setup` 会按锁文件安装依赖并重建原生模块，适合首次拉取仓库后直接使用。

## 常用命令

| 命令                                                              | 说明                             |
| ----------------------------------------------------------------- | -------------------------------- |
| `pnpm run setup`                                                  | 安装依赖并执行原生模块重建       |
| `pnpm run dev`                                                    | 启动桌面应用开发模式             |
| `pnpm run build`                                                  | 类型检查并构建 renderer/main     |
| `pnpm run typecheck`                                              | 仅执行 TypeScript `--noEmit`     |
| `pnpm run test`                                                   | 运行 Vitest 单元测试             |
| `pnpm run rebuild:native`                                         | 仅重建原生模块                   |
| `pnpm run rebuild:native:node`                                    | 将共享原生模块恢复为 Node.js ABI |
| `pnpm --filter @nextshell/desktop run dist --mac --publish never` | 本地打 macOS 包                  |
| `pnpm --filter @nextshell/desktop run dist --win --publish never` | 本地打 Windows 包                |
| `pnpm --filter @nextshell/desktop run dist:linux`                 | 本地打 Linux 包（当前 CPU 架构） |

## Linux 安装与打包

Linux 发布仅构建 x64 的 `.AppImage`、`.deb`、`.tar.gz`，输出在
`apps/desktop/release/`，使用 Ubuntu 22.04 原生构建；旧版发行版仍需单独验证。
适用于 `uname -m` 输出为 `x86_64` 的机器，不提供 Linux ARM 安装包。

### Ubuntu / Debian

在下载目录安装对应版本的 deb（将 VERSION 替换为实际版本）：

```bash
sudo apt install ./NextShell-VERSION-linux-x64.deb
nextshell
```

deb 会声明 GTK、NSS、ALSA、libsecret 等运行依赖。也可以使用 AppImage。

### Arch Linux / 其他常用 Linux 桌面

Arch 可以直接使用 AppImage，无需转换 deb 或自行编译：

```bash
sudo pacman -S --needed gtk3 nss alsa-lib libsecret libnotify libxss libxtst xdg-utils
chmod +x ./NextShell-VERSION-linux-x64.AppImage
./NextShell-VERSION-linux-x64.AppImage
```

若 AppImage 报缺少 `libfuse.so.2`，Arch 安装 `fuse2`；Ubuntu 22.04 安装
`libfuse2`，24.04 安装 `libfuse2t64`。不便使用 FUSE 时，可用
`./NextShell-VERSION-linux-x64.AppImage --appimage-extract-and-run`，或解压
`tar.gz` 后运行目录中的 `./nextshell`（系统仍需安装运行依赖）。

### NixOS

使用 AppImage 兼容环境，不直接运行 tar.gz 内的动态链接二进制。
在 `/etc/nixos/configuration.nix` 中启用：

```nix
{
  programs.appimage.enable = true;
  programs.appimage.binfmt = true;
}
```

应用配置后，在图形会话的终端运行：

```bash
appimage-run ./NextShell-VERSION-linux-x64.AppImage
```

这是 [NixOS 官方的预编译程序运行方式](https://nixos.org/manual/nixos/stable/#sec-custom-packages-prebuilt)。
只在其他发行版安装 Nix 包管理器时，按宿主发行版的安装方式即可。
本地终端如需使用 Nix 提供的 bash/fish/zsh，可在 NextShell 本地 Shell 设置中选择
自定义路径，例如 `/run/current-system/sw/bin/bash`。

### 运行条件与源码构建

NextShell 是桌面应用，可以从桌面终端启动，但纯 SSH/TTY、无显示服务的服务器
不提供 Electron 图形界面；连接这类服务器时，在本机运行 NextShell 再通过 SSH 连接。
钥匙串迁移需要会话 D-Bus 及已解锁的 Secret Service（例如 GNOME Keyring）。
以普通用户启动，保留 Chromium sandbox；遇到发行版的用户命名空间/AppArmor
限制时应配置系统策略，不将 `--no-sandbox` 作为默认启动参数。

源码打包需在目标架构的 Linux 上执行，原生模块不能复用 macOS/Windows 的产物。
Ubuntu 构建依赖：

```bash
sudo apt install build-essential python3 pkg-config libsecret-1-dev libgtk-3-0 libnss3 libasound2-dev
pnpm run setup
pnpm --filter @nextshell/desktop run dist:linux
```

发布 CI 会用打包后的 Electron 加载 SQLite、keytar、SSH，并实际启动 PTY；这不替代
Ubuntu / Arch / NixOS 桌面上的窗口、连接、SFTP 和钥匙串人工验收。

## 原生模块与 ABI 排错

若出现 `NODE_MODULE_VERSION` 不匹配（常见于 `better-sqlite3` / `keytar` / `ssh2`）：

1. 执行 `pnpm run rebuild:native`
2. 若仍失败，执行 `pnpm run setup`
3. 在 Node.js / Electron 版本变化后再次执行 `pnpm run setup`

Electron 与独立 Node.js 进程使用不同 ABI。`pnpm run dev` 会先切换到 Electron ABI；
在 Node.js 下手动运行主进程相关产物前可执行 `pnpm run rebuild:native:node`。

若本地终端打开时报错 `posix_spawnp failed`：

- 这通常是开发环境或未正确重建原生模块的本地运行问题，不是 shell 配置错误，也不表示业务代码回归。
- 在标准 `electron-builder` release 打包流程下通常不会命中，因为打包产物会优先使用 `node-pty/build/Release/spawn-helper`。
- 常见原因是 macOS 开发环境中的 `node-pty/prebuilds/.../spawn-helper` 缺少执行位，导致本地终端无法拉起 shell。

建议按以下顺序恢复：

1. 执行 `pnpm run rebuild:native`
2. 若仍失败，执行 `pnpm run setup`
3. 若问题仍在，删除依赖后重新安装，再重复执行 `pnpm run setup`

## 版本与发布

- 非 tag 本地构建版本格式：`<apps/desktop版本>-dev+<shortSha>`
- 若无法读取 git SHA：`<apps/desktop版本>-dev+unknown`
- CI 发布通过环境变量 `NEXTSHELL_BUILD_VERSION` 注入版本
- Tag 触发工作流：`.github/workflows/release-electron.yml`
- 仅接受 SemVer tag（`v1.2.3` 或 `1.2.3`），且 tag commit 必须位于 `main` 分支祖先链上

## 可选环境变量

- `NEXTSHELL_BUILD_VERSION`: 覆盖构建版本号（CI 发布使用）
- `NEXTSHELL_GITHUB_REPO`: 更新检查目标仓库（默认 `HynoR/NextShell`）
- `VITE_GITHUB_REPO`: 兼容变量，同样用于更新检查仓库配置

## 质量检查建议

提交 PR 前建议至少运行：

```bash
pnpm run typecheck
pnpm run test
```

## 相关文档

- [云同步使用手册](./docs/cloud-sync-user-guide.md)
- [架构说明](./docs/architecture.md)
- [IPC 契约](./docs/ipc-contract.md)

## License

本项目使用 GNU GPLv3，详见 [LICENSE](./LICENSE)。

## Agent MCP 接入

支持 **stdio 无打扰接入**和 **HTTP 直连**。stdio 通过独立 npm 进程提供工具发现，
NextShell 未启动时不影响 Harness 初始化；HTTP 无需 Node.js，但连接时应用必须运行。
两种方式共用应用内权限与执行策略，设置 → Agent 接入可分别复制配置。

配置、依赖与发布前置条件见 [MCP 包说明](packages/mcp/README.md) 和
[插件接入说明](nextshell-plugin/README.md)。
