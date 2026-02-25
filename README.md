# NextShell

NextShell 是一个基于 Electron + React + TypeScript 的桌面运维客户端，使用 Bun Workspace 管理应用与共享包。

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

- Monorepo：Bun Workspace
- 桌面端：Electron（main/preload/renderer）+ React + Vite + Tailwind CSS
- 类型与契约：TypeScript + Zod
- 原生依赖：`better-sqlite3`、`keytar`、`ssh2`

## 项目结构

- `apps/desktop`: Electron 应用入口
- `apps/desktop/src/main`: 主进程服务、IPC 注册、系统集成
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

- Bun `1.3.4`（见 `.bun-version`）
- macOS 或 Windows（CI 默认构建这两个平台）
- 建议安装可用的本地构建工具链，用于编译/重建原生模块

## 快速开始

```bash
bun run setup
bun run dev
```

`bun run setup` 会执行依赖安装并重建原生模块，适合首次拉取仓库后直接使用。

## 常用命令

| 命令 | 说明 |
| --- | --- |
| `bun run setup` | 安装依赖并执行原生模块重建 |
| `bun run dev` | 启动桌面应用开发模式 |
| `bun run build` | 类型检查并构建 renderer/main |
| `bun run typecheck` | 仅执行 TypeScript `--noEmit` |
| `bun test` | 运行工作区 `*.test.ts` |
| `bun run rebuild:native` | 仅重建原生模块 |
| `bun run --cwd apps/desktop dist -- --mac --publish never` | 本地打 macOS 包 |
| `bun run --cwd apps/desktop dist -- --win --publish never` | 本地打 Windows 包 |

## 原生模块与 ABI 排错

若出现 `NODE_MODULE_VERSION` 不匹配（常见于 `better-sqlite3` / `keytar` / `ssh2`）：

1. 执行 `bun run rebuild:native`
2. 若仍失败，执行 `bun run setup`
3. 在 Bun / Electron 版本变化后再次执行 `bun run setup`

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
bun run typecheck
bun test
```

## License

本项目使用 GNU GPLv3，详见 [LICENSE](./LICENSE)。
