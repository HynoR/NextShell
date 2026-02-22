# NextShell

NextShell 是一个 Electron + TypeScript 的桌面运维客户端，采用 Bun 作为工作区与工具链管理。

## Workspace

- `apps/desktop`: Electron 桌面应用（main/preload/renderer）
- `packages/*`: 领域与能力分层包（core/ssh/storage/security/...）
- `docs/*`: 架构、IPC 协议、安全模型文档

## Quick Start

```bash
bun run setup
bun run dev
```

## Native Modules (ABI)

为避免 `better-sqlite3` / `keytar` / `ssh2` 在不同开发环境出现 `NODE_MODULE_VERSION` 不匹配，请使用以下流程：

- 首次拉取仓库后执行 `bun run setup`
- 切换分支后若 lockfile/依赖发生变化，重新执行 `bun run setup`
- Bun 或 Electron 版本变化后，执行 `bun run setup`
- 若已出现 `NODE_MODULE_VERSION` 报错，执行 `bun run rebuild:native` 后再启动

仓库通过 `.bun-version` 固定 Bun 版本（当前为 `1.3.4`），建议本地保持一致。

## Core Features

- 连接管理（分组/搜索/收藏）
- SSH 多标签终端
- SFTP 文件浏览
- 远程资源监控（无 agent）
- 本地存储 + Keychain 敏感信息管理

## Build Versioning

- Release 构建（GitHub Actions tag workflow）通过 `NEXTSHELL_BUILD_VERSION` 注入版本，值来自 git tag（支持 `v1.2.3` / `1.2.3`）。
- 非 tag 构建默认使用 `apps/desktop/package.json` 的基础版本拼接短 commit SHA：`<base>-dev+<shortSha>`。
- 如果无法读取 git SHA，则回退为 `<base>-dev+unknown`。
- 应用内版本显示、更新检查当前版本、备份元数据 `appVersion` 均使用同一构建版本来源。
