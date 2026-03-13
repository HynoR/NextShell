# Goal Sync Working Set

本目录用于固化“通用云同步重构”在当前阶段已经对齐的目标、约束、接口草案、分阶段开发 TODO 和验收标准，避免后续分阶段开发时遗漏上下文。

## 文档清单

- `01-approved-decisions.md`
  - 当前聊天中已经确认的产品约束、默认策略和非目标。
- `02-system-architecture.md`
  - 客户端资源模型、多 workspace 运行时、复制/移动/删除/回收站行为设计。
- `03-api-and-contracts.md`
  - 服务端 API v2 草案、IPC 草案、前后端共享类型草案。
- `04-phased-build-todo.md`
  - 推荐的分阶段实施顺序、阶段目标、交付物、每阶段退出条件。
- `05-acceptance-and-risk-checklist.md`
  - 验收目标、测试矩阵、风险点、回归重点。

## 建议阅读顺序

1. `01-approved-decisions.md`
2. `02-system-architecture.md`
3. `03-api-and-contracts.md`
4. `04-phased-build-todo.md`
5. `05-acceptance-and-risk-checklist.md`

## 使用方式

- 新开一个开发阶段前，先确认本目录里的约束没有变化。
- 新增实现范围时，优先修改对应文档，再开始写代码。
- 如果后续产品方向调整，优先更新 `01-approved-decisions.md`，再同步改其它文档。

## 当前总原则

- 防丢失优先于防冗余。
- 默认跨来源操作一律复制，不默认移动。
- 任何删除或覆盖都必须先留存旧版本。
- 回收站默认只手动清空。
- 旧本地数据必须可继续读取，不自动覆盖、不自动改来源。
