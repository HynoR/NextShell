import { isExternalFileDrag } from "../../../utils/sftpFileDrop";
import type { ResourceTab } from "../types";

/**
 * 管理器内部拖连接行的自定义 dataTransfer 类型。用私有 MIME 而不是 `text/plain`,
 * 是为了让它与"从桌面拖 JSON 文件进来导入"在判定上互不干扰:内部拖拽不带 `Files`,
 * 文件拖入不带这个类型,两个 overlay 永远不会同时点亮。
 */
export const CONNECTION_DRAG_MIME = "application/x-nextshell-connections";

type DragTypesLike = {
  types?: Iterable<string> | ArrayLike<string>;
};

/** dataTransfer 里是不是管理器自己发起的连接拖拽。 */
export const isInternalConnectionDrag = (
  dataTransfer: DragTypesLike | null | undefined
): boolean => {
  if (!dataTransfer?.types) {
    return false;
  }
  return Array.from(dataTransfer.types).includes(CONNECTION_DRAG_MIME);
};

export const serializeConnectionDragIds = (connectionIds: readonly string[]): string =>
  JSON.stringify(connectionIds);

/**
 * 解析拖拽负载。拖进来的东西不受本进程控制(理论上可以是别处的同名类型),
 * 解析失败一律当"没有连接"处理,而不是抛异常把 drop 事件炸掉。
 */
export const parseConnectionDragIds = (raw: string | null | undefined): string[] => {
  if (!raw) {
    return [];
  }
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) {
      return [];
    }
    return parsed.filter((item): item is string => typeof item === "string" && item.length > 0);
  } catch {
    return [];
  }
};

/**
 * 拖文件进管理器 = 导入。判定抽成纯函数是因为它有四个否定条件,漏掉任何一个都会变成
 * "拖进去没反应"或"在云作用域下静默导入到本地"这类无痕故障。
 */
export interface ManagerFileDropContext {
  open: boolean;
  resourceTab: ResourceTab;
  /** 上一批预览还在跑时不接新文件,否则两个队列会互相顶掉。 */
  importingPreview: boolean;
  /** 导入执行链路只写本地,云作用域下必须拒绝。 */
  scopeKind: "local" | "cloud";
}

export const canAcceptManagerFileDrop = ({
  open,
  resourceTab,
  importingPreview,
  scopeKind
}: ManagerFileDropContext): boolean =>
  open && resourceTab === "connections" && !importingPreview && scopeKind === "local";

/**
 * `.cm2-shell` 上的文件拖放监听要不要在**捕获相**把这次拖拽截下来。
 *
 * rc-tree 给每个可拖节点都挂了无条件 `preventDefault + stopPropagation` 的 dragover/drop,
 * 文件松在目录节点上时事件根本冒泡不到外层的导入监听——松在根节点或空白区却正常,于是
 * 表现成"有时候能导入有时候不能"这种最难查的无痕故障。所以文件拖入必须在捕获相就判定
 * 并截断,让 rc-tree 完全看不到它。
 *
 * 反过来,内部连接拖拽(私有 MIME)与目录自身的拖拽绝不能截:截了它们就没有落点了。
 * 判定完全靠 `isExternalFileDrag`——它只认 `Files`,两类内部拖拽都不带。
 */
export const shouldInterceptFileDrag = (
  canAcceptDrop: boolean,
  dataTransfer: Parameters<typeof isExternalFileDrag>[0]
): boolean => canAcceptDrop && isExternalFileDrag(dataTransfer);

/**
 * 拖进来了但一个路径都拿不到。两种原因要分开说:Electron 在某些来源(压缩包内、远程磁盘)
 * 读不到真实路径,这时用户该改用按钮;拖的是目录或文本则是另一回事。
 */
export const describeManagerDropWarning = ({
  allPathsEmpty
}: {
  allPathsEmpty: boolean;
}): string =>
  allPathsEmpty ? "无法读取拖入文件的路径，请改用「导入」按钮选择文件" : "当前仅支持拖入文件";
