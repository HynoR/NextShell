# NextShell

NextShell 是一个 Electron + TypeScript 的桌面运维客户端原型工程，采用 Bun 作为工作区与工具链管理。

## Workspace

- `apps/desktop`: Electron 桌面应用（main/preload/renderer）
- `packages/*`: 领域与能力分层包（core/ssh/storage/security/...）
- `docs/*`: 架构、IPC 协议、安全模型文档

## Quick Start

```bash
bun install
bun run dev
```

## MVP Scope

- 连接管理（分组/搜索/收藏）
- SSH 多标签终端
- SFTP 文件浏览
- 远程资源监控（无 agent）
- 本地存储 + Keychain 敏感信息管理
