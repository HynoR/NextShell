# AGENTS.md

## Cursor Cloud specific instructions

### Project overview

NextShell 是一个基于 Electron + React + TypeScript 的桌面 SSH/运维客户端，使用 Bun Workspace 管理 monorepo。详细的项目结构、命令列表和原生模块排错指南见 `README.md`。

### Runtime requirements

- **Bun 1.3.4**（见 `.bun-version`）
- **libsecret-1-dev** 和 **build-essential**（编译 `keytar`、`better-sqlite3`、`ssh2`、`node-pty` 等原生模块）
- Electron 在无头 Linux 环境下运行时依赖 Xvfb（`DISPLAY=:1`）

### Common dev commands

参见 `README.md` 的「常用命令」表格。核心命令：

| 命令 | 说明 |
|---|---|
| `bun run setup` | 安装依赖并重建原生模块（首次或 Electron/Bun 版本变更后执行） |
| `bun run dev` | 启动 Electron 开发模式（Vite dev server on port 5173） |
| `bun run typecheck` | TypeScript `--noEmit` 检查 |
| `bun test` | 运行工作区所有 `*.test.ts` |

### Gotchas for Cloud VM

- Electron 启动时会输出 D-Bus 相关 ERROR 日志（`Failed to connect to the bus`）和 WebGL2 blocklisted 错误，这些在无头 Linux 环境下是正常的，不影响功能。
- `keytar` 在没有桌面密钥环（gnome-keyring）的环境下可能回退到不安全存储，应用会自动生成 device key 作为替代。
- `bun run dev` 会先通过 Vite 构建 main/preload，然后自动启动 Electron 窗口。
- 原生模块（`better-sqlite3`, `keytar`, `ssh2`, `node-pty`）需要针对 Electron 的 Node ABI 重建，`bun run setup` 已包含此步骤。如果只需要重建原生模块，可以单独执行 `bun run rebuild:native`。
