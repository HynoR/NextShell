# Renderer 层 Playwright 复现指南(无 Electron)

> 写给后续在本仓库工作的 agent。这套方法在 2026-07-29 实战定位了「多 session 终端串屏/冻结」bug(根因:xterm 缺 `allowProposedApi` 导致 `registerDecoration` 在解析循环内抛异常、写队列永久死亡),从搭建到抓到异常栈全程无需启动 Electron。

## 适用场景与原理

渲染进程是纯 web 应用,与外界的唯一边界是 preload 注入的 `window.nextshell`(完整类型见 `packages/shared/src/api.ts`)。把这个对象 mock 掉,真实的 renderer 代码(React 组件、Zustand store、xterm、OSC runtime)就能原样跑在普通 Chromium 里,由 Playwright MCP 驱动和取证。

适合:终端渲染/回放/切 tab、store 逻辑、xterm 集成类 bug。不适合:主进程 SSH/SFTP/keytar/连接池本身的 bug(那些逻辑被 mock 掉了)。

**重要约束:不要尝试 `pnpm dev` 或以任何方式启动 Electron 来做自动化**——会被运行沙箱/权限分类器拦截,而且 dev 应用共享用户真实的连接数据库和钥匙串,有污染与弹窗风险。这套 renderer-only 方案就是为绕开这些而设计的。

## 现成资产

| 文件 | 作用 |
|---|---|
| `apps/desktop/vite.renderer-repro.config.ts` | 去掉 electron 插件的 vite 配置,只起 renderer dev server |
| `apps/desktop/scripts/renderer-repro/setup-mock.mjs` | 注入 `window.nextshell` mock(内置假 shell,发 OSC 133/7)并加载页面 |
| `apps/desktop/scripts/renderer-repro/drive-two-sessions.mjs` | 双 session 冒烟:开两 tab、A 打字、切 B、再切回,四张截图 |

## 快速开始(4 步)

1. **起 renderer dev server**(后台运行):
   ```bash
   cd apps/desktop && pnpm exec vite --config vite.renderer-repro.config.ts
   ```
   等 `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:5173/` 返回 200。端口必须 5173(index.html 的 CSP 只放行它)。

2. **注入 mock 并加载页面**:调用 Playwright MCP 的 `browser_run_code_unsafe`,`filename` 传 `setup-mock.mjs` 的绝对路径。返回值里 `rootText` 应包含侧栏文案(「服务器/设置」),`errors` 应为空。

3. **跑场景**:同样方式运行 `drive-two-sessions.mjs`(或照它改写你的场景)。截图落在 `<仓库根>/.playwright-mcp/`(已 gitignore)。

4. **取证**:
   - 截图用 Read 直接看(图像可读);
   - `browser_console_messages`(level: error)拿 console 与未捕获异常;
   - setup 脚本挂了 `page.on("pageerror")`,driver 返回值的 `errors` 数组里有完整异常栈——**这是定位渲染层崩溃的第一手证据**(2026-07 那次就是靠它抓到 `allowProposedApi` 异常栈的);
   - `window.__fakeState.ackLog` 可查流控 ack 是否还在流动(冻结时 ack 会停摆)。

## MCP 硬性约束(踩过的坑)

- `browser_run_code_unsafe` 的 `filename` 只能读仓库根和 `<仓库根>/.playwright-mcp/` 下的文件。
- 脚本文件必须是**一个裸的 `async (page) => {...}` 表达式**(运行器会包成 `await (<文件内容>)(page)`):不能有 `export`,收尾大括号后不能有分号或其他语句;顶部注释没问题。
- 截图路径写绝对路径,放 `.playwright-mcp/` 下。
- `addInitScript` 必须在 `goto` 之前注册;同一 page 的后续 `reload()`/`goto` 仍生效,所以改完源码(vite HMR 自动生效)后 driver 里 `page.reload()` 即可重测。

## Mock 的关键设计(改造时别破坏)

- **`settings.get` 故意 reject**:`usePreferencesStore` 失败时会退回 `DEFAULT_APP_PREFERENCES`,省去伪造完整偏好对象。
- **数据流仿真主进程 dispatcher**:`deliveryId` 全局递增(跨 session 共享计数器)、`byteLength` 用 `TextEncoder` 精确计算、`setTimeout` 异步分帧投递、`session.open` 先发数据再 resolve(复现「数据先于 store 知道 session」的真实时序)。
- **假 shell 会发 OSC `133;A/B/C/D` 和 OSC 7**,等价于开了 shell integration 的远端;`seq N` 命令产生大量输出压测流路径。
- **连接的 `monitorSession: true`** 会启用终端 compat guards 等按连接开关的路径。
- 漏 mock 的 API 会在 console 报 TypeError,照 `packages/shared/src/api.ts` 补上即可。

## 直接操纵 store

Vite dev 下页面内 `await import("/src/renderer/store/useWorkspaceStore.ts")` 拿到的是**与 App 同一个** store 单例,可以直接 `upsertSession`/`setActiveSession` 造状态,绕过 UI 点击。键入则走真实路径:点 `.terminal-shell .xterm` 聚焦后 `page.keyboard.type(...)`;切 tab 点 `.session-tab`。

## 真实应用的错误取证

renderer 的全局错误(`window.error`、`unhandledrejection`、React 错误边界)现在会经 `debug.reportRendererError` IPC 转发进主进程日志(`~/Library/Logs/@nextshell/desktop/main.log`,前缀 `[RendererError]`,每次启动最多 30 条)。**排查用户报告的 UI 异常/冻结时,先查这份日志**;2026-07 之前的版本没有这条链路,渲染层崩溃在日志里完全无痕。

## 用完清理

停掉后台 vite(TaskStop)、`browser_close`;`.playwright-mcp/` 已 gitignore,可留可删。两个脚本与 vite 配置是长期资产,不要删。

## 实例存档:2026-07 多 session 冻结

现象:同连接双开,tab-A 打字后切 tab-B,A 的内容残留、无法清除/输入。用上面这套流程跑 `drive-two-sessions.mjs`,截图显示 tab-A 在第一条命令回车后画面冻结,`errors` 抓到 `registerDecoration` 的 `allowProposedApi` 异常栈——OSC `133;D` 触发 proposed API 抛异常,击穿 xterm `WriteBuffer._innerWrite` 后写循环永不再调度,单一共享 xterm 上所有 session 一起死。修复:构造 xterm 加 `allowProposedApi: true` + `renderer/terminal/parserGuards.ts` 包裹全部 parser handler + `oscRuntime` write 回调兜底。回归验证即本文档的 4 步流程。
