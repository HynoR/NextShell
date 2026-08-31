import type { DetailMode } from "../types";

/**
 * 编辑态的离开语义。抽成纯函数是为了能单测:这几条规则决定了「填了一半的表单会不会没」
 * 以及「别人的表单会不会被顺手关掉」,而它们在组件里都埋在事件回调深处。
 */

/** 只有「正在编辑」且「改过」才值得打断用户;其余情况直接放行。 */
export const shouldConfirmDiscard = (editing: boolean, dirty: boolean): boolean => editing && dirty;

/** 右栏当前挂着的连接 id。空态没有;编辑态的「新建」也没有(还没落库)。 */
export const detailConnectionId = (detail: DetailMode): string | undefined =>
  detail.kind === "empty" ? undefined : detail.connection?.id;

/**
 * 这批被改动的 id 里有没有右栏正挂着的那一条。两个调用点:
 * - 拖拽移动/重命名:命中就必须先离开编辑态,否则编辑器里那份打开时的快照会在保存时
 *   把这次改动整份写回旧值——移动被静默回滚,没有任何报错。
 * - 删除:只有命中才该清空右栏,否则删 A 会把正在编辑的 B 一起关掉。
 */
export const affectsDetailConnection = (
  connectionId: string | undefined,
  affectedIds: readonly string[]
): boolean => connectionId !== undefined && affectedIds.includes(connectionId);

export type ManagerCancelIntent = "leaveEdit" | "closeDialog";

/**
 * antd 的 Modal 把 Esc、点 X、点遮罩合成同一个 onCancel,只能靠事件类型区分:
 * rc-dialog 的 Esc 来自 window keydown,关闭按钮与遮罩都是 click。
 * 设计 5.3:编辑态 Esc 退回只读详情(不关弹窗),其余一律关弹窗。
 */
export const resolveCancelIntent = (
  eventType: string | undefined,
  editing: boolean
): ManagerCancelIntent => (eventType === "keydown" && editing ? "leaveEdit" : "closeDialog");
