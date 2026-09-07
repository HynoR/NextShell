export const meta = {
  name: 'nextshell-improvement-sweep',
  description: 'Four-dimension analysis (UI/UX, IPC, perf, build) with adversarial verification, producing a prioritized implementable work list',
  phases: [
    { title: 'Analyze', detail: '4 parallel dimension analysts' },
    { title: 'Verify', detail: 'adversarial per-finding verification against current HEAD' },
  ],
}

const CTX = `
## 仓库背景(务必先读,避免重复劳动)

仓库:/Users/ztwang/repo/nextshell — NextShell,Electron 桌面 SSH/SFTP 客户端,pnpm workspace monorepo。
- 主进程 apps/desktop/src/main/(ssh2、better-sqlite3、keytar;服务在 ServiceContainer,IPC 经 src/main/ipc/registry.ts 表驱动注册 + register.ts 通用循环,Zod 校验)
- preload apps/desktop/src/preload/(contextBridge 暴露 window.nextshell)
- 渲染层 apps/desktop/src/renderer/(React 19 + Ant Design + Zustand,中文界面,深/浅主题)
- packages/shared 是三进程契约(channels.ts / contracts.ts / api.ts);packages/core|storage|ssh|security|terminal|ui-kit
- 类型检查门禁:pnpm run typecheck。单元测试由 Vitest 聚合(pnpm run test)。

## 已完成的既往工作(不要再提这些!)

1. IPC 专项(见 /Users/ztwang/GolandProjects/nextshell/IPC_OPTIMIZATION_PLAN.md,可读):
   - 稳定性三件套:ErrorBoundary + 全局错误处理、render-process-gone 自动 reload、powerMonitor/窗口隐藏联动暂停监控轮询、dispatcher stall 超时。
   - 死 IPC 垂直切片已删;终端解码 UTF-8 快路径;dispatcher 增量字节数;sessionOutputBuffer 单次编码;TerminalPane 查找缓存。
   - 监控流已去 ack 化(裸 send);终端流已改滑动窗口 512KB + 攒批 ack(128KB/50ms)。
   - ServiceContainer 已扁平化为 11 个子服务属性;register.ts 已表驱动(registry.ts + registry.test.ts 97 channel 对齐)。
   - storage 层孤儿仓储方法已删(commit 9eb0334)。
2. UI/UX 已有完整审计报告 /Users/ztwang/GolandProjects/nextshell/docs/ui-ux-audit.md(115 条 finding,带稳定 ID、file:line、严重度、建议)。

## 你的行为约束

- 只读分析,不要修改任何文件。
- 每条发现必须给出你亲自核实过的当前代码 file:line 证据(审计/计划文档可能已过时)。
- 输出用中文。findings 按价值从高到低排。
`

const FINDINGS_SCHEMA = {
  type: 'object',
  required: ['findings'],
  properties: {
    findings: {
      type: 'array',
      maxItems: 8,
      items: {
        type: 'object',
        required: ['id', 'title', 'value', 'effort', 'risk', 'files', 'description', 'fix_sketch'],
        properties: {
          id: { type: 'string', description: 'kebab-case 稳定 ID' },
          title: { type: 'string', description: '中文一句话标题' },
          value: { enum: ['high', 'medium', 'low'] },
          effort: { enum: ['S', 'M', 'L'] },
          risk: { enum: ['low', 'medium', 'high'], description: '施工回归风险' },
          files: { type: 'array', items: { type: 'string' }, description: '涉及的文件(相对仓库根)' },
          description: { type: 'string', description: '问题描述 + 亲自核实的 file:line 证据' },
          fix_sketch: { type: 'string', description: '具体施工方案(改哪些文件、怎么改、怎么验证)' },
        },
      },
    },
  },
}

const VERDICT_SCHEMA = {
  type: 'object',
  required: ['exists_now', 'worth_doing', 'safe', 'notes'],
  properties: {
    exists_now: { type: 'boolean', description: '问题在当前 HEAD 代码中确实存在' },
    worth_doing: { type: 'boolean', description: '修复带来的价值明显大于成本与风险' },
    safe: { type: 'boolean', description: '按 fix_sketch 施工不会引入明显回归' },
    notes: { type: 'string', description: '核实过程、发现的偏差、对 fix_sketch 的修正建议(中文)' },
  },
}

const DIMENSIONS = [
  {
    key: 'uiux',
    prompt: `${CTX}
## 任务:UI/UX 维度 — 从已有审计中甄选最高价值的可施工项

/Users/ztwang/GolandProjects/nextshell/docs/ui-ux-audit.md 是一份 115 条 finding 的完整审计(1159 行,分页读完,至少读完执行摘要、9 条系统性主题、全部 🔴/🟠 条目)。你的任务不是重新审计,而是**甄选 + 核实 + 打包**:

1. 从审计中挑出价值最高、且可以在无人值守下安全施工的项(优先 S/M 工作量、低回归风险;排除审计开头明确标注"建议本地边看边验时再做"的大 IA 重构和深色优先翻转)。
2. 对每个候选项,打开当前代码核实:问题是否仍存在?file:line 是否漂移?(审计后代码有过多次重构)
3. 把关联小项打包成连贯的工作包(例如:"本地化与反馈统一"可以打包 antd zhCN locale + 静态 message/Modal 迁移到 App.useApp;"死代码清理"可以打包 DiskMonitorPane + 无引用 CSS + 幽灵变量)。每个工作包作为一条 finding 输出,description 里列全子项。
4. 价值判断标准:用户每天感知到的摩擦 > 一次性迷惑 > 纯代码卫生。

输出最多 8 个工作包,按价值排序。`,
  },
  {
    key: 'ipc',
    prompt: `${CTX}
## 任务:IPC 维度 — 大规模重构后的新一轮体检

先读 IPC_OPTIMIZATION_PLAN.md 了解已完成的 Phase 0-4(那些都不要再提)。然后以新鲜视角体检当前 IPC 全链路,找**新的**改进点:

重点方向(不限于):
1. registry.ts 表驱动化之后的类型安全:dispatch 返回值与 packages/shared/src/api.ts 声明之间是否有编译期约束?schema 与 api.ts 参数类型是否可能漂移?有无办法用类型体操把三者锁死?
2. 错误处理一致性:handler 抛错到 renderer 后的呈现路径;错误是否带可诊断信息;有无吞错。
3. 事件流侧(webContents.send 的所有通道):preload .on 的订阅/退订是否有泄漏风险;renderer 端监听器生命周期。
4. Zod 校验在高频路径上的开销;schema 复用与 parse 缓存。
5. preload 层:api 表面是否还有冗余;contextBridge 暴露的对象结构。
6. 安全:sender 校验(是否任意 frame 都能 invoke)、通道白名单。
7. 剩余的性能点:大 payload 序列化(SFTP list 大目录、monitor 快照结构)。

每条必须是当前代码里亲自核实的,给 file:line。输出最多 8 条,按价值排序。`,
  },
  {
    key: 'perf',
    prompt: `${CTX}
## 任务:性能维度 — 渲染层与主进程全面扫描

终端数据热路径已优化过(见既往工作,不要再提)。找**新的**性能改进点,重点方向(不限于):

1. React 渲染性能:Zustand 订阅粒度(是否有组件订阅整个 store 导致每帧重渲染)、列表虚拟化(SFTP 大目录、传输队列、命令历史)、昂贵组件的 memo 缺失、context 导致的级联重渲染。用 grep 找 useWorkspaceStore() 无 selector 的调用。
2. 启动性能:vite 产物是否分包、重量级依赖(antd、xterm、编辑器)是否懒加载、preload/main 的启动串行点。可以跑 pnpm run build 看产物体积(允许构建,但不要改代码)。
3. 轮询与定时器:各处 setInterval/轮询的频率、可见性感知是否完备(pollingScheduler 是否被所有轮询方使用)。
4. 主进程:better-sqlite3 查询模式(N+1、缺索引、每次全表)、keytar 调用频率、SFTP 目录列举的排序/序列化。
5. 内存:事件监听器累积、Map/数组只增不减、闭包持有大 buffer。

每条给出亲自核实的 file:line 证据和量化影响估计(哪个用户操作、多大规模时能感知)。输出最多 8 条,按价值排序。`,
  },
  {
    key: 'build',
    prompt: `${CTX}
## 任务:软件构建维度 — 构建/工程化/发布链路扫描

这个维度此前从未系统分析过。扫描范围:

1. 构建配置:apps/desktop 的 vite/electron 构建配置(electron.vite.config? vite.config?)、tsconfig 体系(有无 project references、增量编译)、产物体积与分包、sourcemap 策略。
2. 打包发布:electron-builder 配置(asar、签名、目标平台、文件过滤是否把不必要的东西打进产物 — 常见坑:整个 node_modules、未用的 locale、map 文件)、dist 产物大小。
3. 开发体验:pnpm run dev 的启动链路、typecheck 覆盖范围(是否所有 packages 都被检查)、Vitest 与 Node 集成测试边界、lint/format 是否缺失。
4. CI:.github/workflows 现状(b5b86a7 提到过 CI),typecheck/test 是否都在 CI 里跑。
5. 依赖健康:package.json 里未使用的依赖、重复功能依赖、依赖放错位置(devDependencies vs dependencies — 对 electron-builder 打包体积有直接影响)、pnpm-lock.yaml 与 workspace 协议。

允许跑只读命令(ls、cat、pnpm run typecheck、du 等)和 pnpm run build 观察产物,但不要修改任何文件。每条给出证据与量化收益。输出最多 8 条,按价值排序。`,
  },
]

function verifyPrompt(d, f) {
  return `${CTX}
## 任务:对抗核实一条候选改进(维度:${d.key})

另一位分析师提出了如下改进点,你的默认立场是**怀疑并试图否决它**。打开当前代码逐条核实。

- ID: ${f.id}
- 标题: ${f.title}
- 声称价值/工作量/风险: ${f.value} / ${f.effort} / ${f.risk}
- 涉及文件: ${(f.files || []).join(', ')}
- 问题描述: ${f.description}
- 施工方案: ${f.fix_sketch}

核实清单:
1. exists_now — 到声称的 file:line 亲自看,问题当前是否真实存在?(注意:仓库最近有过大规模 IPC 重构和清理,分析师可能引用了过时状态)
2. worth_doing — 价值是否被夸大?是否与既往已完成工作重复?是否属于审计里明确标注需要"本地边看边验"的高风险大重构?
3. safe — 按 fix_sketch 施工是否会破坏现有行为?检查方案里提到的每个文件是否存在、方案与现有代码结构是否兼容、有没有被忽略的调用方/依赖。
4. notes 里写清你的核实证据(file:line)、发现的偏差、以及对 fix_sketch 的修正建议。若方案基本对但细节有误,exists_now/worth_doing/safe 仍可为 true,把修正写进 notes。

只读,不要修改文件。`
}

phase('Analyze')
log('四路维度分析启动:uiux / ipc / perf / build')

const results = await pipeline(
  DIMENSIONS,
  (d) => agent(d.prompt, { label: `analyze:${d.key}`, phase: 'Analyze', schema: FINDINGS_SCHEMA, effort: 'high' }),
  (res, d) => {
    if (!res || !res.findings || res.findings.length === 0) {
      log(`${d.key}: 分析无产出`)
      return []
    }
    log(`${d.key}: ${res.findings.length} 条候选,进入对抗核实`)
    return parallel(
      res.findings.map((f) => () =>
        agent(verifyPrompt(d, f), {
          label: `verify:${d.key}:${f.id}`,
          phase: 'Verify',
          schema: VERDICT_SCHEMA,
          effort: 'medium',
        }).then((v) => ({ dimension: d.key, ...f, verdict: v }))
      )
    )
  }
)

const all = results.filter(Boolean).flat().filter(Boolean)
const ok = (f) => f.verdict && f.verdict.exists_now && f.verdict.worth_doing && f.verdict.safe
const confirmed = all.filter(ok)
const risky = all.filter((f) => f.verdict && f.verdict.exists_now && f.verdict.worth_doing && !f.verdict.safe)
const rejected = all.filter((f) => !f.verdict || !f.verdict.exists_now || !f.verdict.worth_doing)

log(`核实完成:confirmed=${confirmed.length} risky=${risky.length} rejected=${rejected.length}`)

return {
  confirmed,
  risky,
  rejected: rejected.map((f) => ({ dimension: f.dimension, id: f.id, title: f.title, notes: f.verdict ? f.verdict.notes : 'agent 无产出' })),
}
