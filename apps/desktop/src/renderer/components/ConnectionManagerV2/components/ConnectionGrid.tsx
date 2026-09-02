import { memo, useCallback, useImperativeHandle, useRef, useState } from "react";
import type { DragEvent, MouseEvent, RefObject } from "react";
import { Tooltip } from "antd";
import type { ConnectionFolder } from "@nextshell/core";
import {
  CONNECTION_DRAG_MIME,
  isInternalConnectionDrag,
  parseConnectionDragIds,
  serializeConnectionDragIds
} from "../utils/managerDrop";
import type { GridConnectionItem, GridFolderItem, GridSection } from "../utils/gridItems";

/** 外部定位到某个连接时用。签名与休眠的表格保持一致——深链那条路径不必知道中栏换了形态。 */
export interface ConnectionGridHandle {
  /** `ancestorFolderRowKeys` 是表格时代的展开参数,网格没有折叠层级,忽略即可。 */
  revealConnection: (connectionId: string, ancestorFolderRowKeys: readonly string[]) => void;
}

interface ConnectionGridProps {
  sections: GridSection[];
  /** 空态文案。搜索无命中与空目录是两回事,由调用方决定说哪句。 */
  emptyText: string;
  selectedIds: string[];
  focusedId?: string;
  /** 单击连接磁贴:选中 + 右栏详情。调用方负责过编辑守卫(两件事要么一起发生要么都不发生)。 */
  onFocus: (connectionId: string) => void;
  /** Ctrl/Cmd+单击:多选增减。不动右栏,所以不需要守卫。 */
  onToggleSelect: (connectionId: string) => void;
  /** 点空白/点目录磁贴:清空连接选中。 */
  onClearSelection: () => void;
  /** 双击连接磁贴:关闭对话框并直连。 */
  onConnect: (connectionId: string) => void;
  onRowContextMenu: (event: MouseEvent, connectionId: string) => void;
  /** 双击目录磁贴:钻取进去。 */
  onEnterFolder: (folderId: string) => void;
  onFolderContextMenu: (event: MouseEvent, folder: ConnectionFolder) => void;
  /** 把一批连接拖到某个目录磁贴上。 */
  onDropConnections: (connectionIds: string[], folderId: string) => void;
  gridRef?: RefObject<ConnectionGridHandle | null>;
}

/**
 * 拖多条时给一个"移动 N 个"的跟随标签,否则用户只看到一个磁贴在飞。
 * (休眠的 ConnectionTable 里有一份同样的实现;等它被删掉时这份就是唯一的了。)
 */
const attachDragGhost = (event: DragEvent<HTMLElement>, count: number): void => {
  if (count < 2 || typeof document === "undefined") {
    return;
  }
  const ghost = document.createElement("div");
  ghost.className = "cm2-drag-ghost";
  ghost.textContent = `移动 ${count} 个连接`;
  document.body.appendChild(ghost);
  event.dataTransfer.setDragImage(ghost, 12, 12);
  // 立刻移除会让某些平台抓不到快照，交给下一个任务队列。
  window.setTimeout(() => ghost.remove(), 0);
};

/**
 * Finder 式图标网格(D18)。整块磁贴都是点击目标——实测反馈是表格行里的小按钮"跟玩 FPS
 * 一样得非常准才点得了",所以这里既没有行内按钮也没有勾选框,交互全部落在磁贴本身:
 * 单击选中、Ctrl/Cmd+单击加减、双击直连/钻取、右键菜单、拖拽移动。
 */
const ConnectionGridInner = ({
  sections,
  emptyText,
  selectedIds,
  focusedId,
  onFocus,
  onToggleSelect,
  onClearSelection,
  onConnect,
  onRowContextMenu,
  onEnterFolder,
  onFolderContextMenu,
  onDropConnections,
  gridRef
}: ConnectionGridProps) => {
  const wrapRef = useRef<HTMLDivElement>(null);
  const [dropFolderId, setDropFolderId] = useState<string>();

  useImperativeHandle(
    gridRef,
    () => ({
      revealConnection: (connectionId) => {
        // 磁贴是这一轮 setState 才出现的,等一帧再找它;找不到就安静放弃——选中与右栏
        // 详情已经由调用方设好了,滚动只是锦上添花,不该为此抛异常。
        window.requestAnimationFrame(() => {
          // 逐个比对 dataset 而不是拼选择器:id 来自数据库,拼进选择器还要考虑转义,
          // 而这里的候选最多几十个,遍历完全不值得为此引入一条会抛异常的路径。
          const nodes = wrapRef.current?.querySelectorAll<HTMLElement>("[data-connection-id]");
          const node = Array.from(nodes ?? []).find(
            (candidate) => candidate.dataset.connectionId === connectionId
          );
          if (node && typeof node.scrollIntoView === "function") {
            node.scrollIntoView({ block: "center" });
          }
        });
      }
    }),
    []
  );

  const handleTileClick = useCallback(
    (event: MouseEvent<HTMLElement>, connectionId: string) => {
      if (event.metaKey || event.ctrlKey) {
        onToggleSelect(connectionId);
        return;
      }
      onFocus(connectionId);
    },
    [onFocus, onToggleSelect]
  );

  const handleDragStart = useCallback(
    (event: DragEvent<HTMLElement>, connectionId: string) => {
      // 拖的是选区里的一条 → 拖整个选区；拖的是选区外的一条 → 只拖它自己。
      const ids = selectedIds.includes(connectionId) ? selectedIds : [connectionId];
      event.dataTransfer.setData(CONNECTION_DRAG_MIME, serializeConnectionDragIds(ids));
      event.dataTransfer.effectAllowed = "move";
      attachDragGhost(event, ids.length);
    },
    [selectedIds]
  );

  const handleFolderDragOver = useCallback((event: DragEvent<HTMLElement>, folderId: string) => {
    if (!isInternalConnectionDrag(event.dataTransfer)) {
      return;
    }
    // 不 preventDefault 就没有 drop 事件；只对自己的拖拽类型这么做，文件拖入照旧走外层遮罩。
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer.dropEffect = "move";
    setDropFolderId(folderId);
  }, []);

  const handleFolderDrop = useCallback(
    (event: DragEvent<HTMLElement>, folderId: string) => {
      setDropFolderId(undefined);
      if (!isInternalConnectionDrag(event.dataTransfer)) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      const ids = parseConnectionDragIds(event.dataTransfer.getData(CONNECTION_DRAG_MIME));
      if (ids.length > 0) {
        onDropConnections(ids, folderId);
      }
    },
    [onDropConnections]
  );

  // 点在磁贴之间的空白上 = 取消选中。落在磁贴上的点击由磁贴自己处理,这里放行。
  const handleBackdropClick = useCallback(
    (event: MouseEvent<HTMLDivElement>) => {
      if (!(event.target instanceof Element) || !event.target.closest(".cm2-tile")) {
        onClearSelection();
      }
    },
    [onClearSelection]
  );

  const renderFolder = (item: GridFolderItem) => (
    <button
      key={item.key}
      type="button"
      className={`cm2-tile cm2-tile--folder${
        item.folder.id === dropFolderId ? " cm2-tile--drop" : ""
      }`}
      data-folder-id={item.folder.id}
      onClick={(event) => {
        if (!event.metaKey && !event.ctrlKey) {
          onClearSelection();
        }
      }}
      onDoubleClick={() => onEnterFolder(item.folder.id)}
      onContextMenu={(event) => {
        event.preventDefault();
        onFolderContextMenu(event, item.folder);
      }}
      onDragOver={(event) => handleFolderDragOver(event, item.folder.id)}
      onDragLeave={() =>
        setDropFolderId((previous) => (previous === item.folder.id ? undefined : previous))
      }
      onDrop={(event) => handleFolderDrop(event, item.folder.id)}
    >
      <span className="cm2-tile-icon">
        <i className="ri-folder-3-fill" aria-hidden="true" />
      </span>
      <span className="cm2-tile-name">{item.folder.name}</span>
      <span className="cm2-tile-sub">{item.count} 台</span>
    </button>
  );

  const renderConnection = (item: GridConnectionItem) => {
    const { connection, authMissing } = item.row;
    const selected = selectedIds.includes(connection.id);
    return (
      <Tooltip
        key={item.key}
        // 磁贴上只放 host(D18:不显示端口),完整地址交给悬浮提示。延迟给得足够长,
        // 免得鼠标扫过一片网格时一路弹出气泡。
        title={`${connection.username ? `${connection.username}@` : ""}${connection.host}:${connection.port}`}
        mouseEnterDelay={0.4}
      >
        <button
          type="button"
          className={`cm2-tile cm2-tile--connection${selected ? " cm2-tile--selected" : ""}${
            connection.id === focusedId ? " cm2-tile--focused" : ""
          }`}
          data-connection-id={connection.id}
          draggable
          onDragStart={(event) => handleDragStart(event, connection.id)}
          onClick={(event) => handleTileClick(event, connection.id)}
          onDoubleClick={() => onConnect(connection.id)}
          onContextMenu={(event) => onRowContextMenu(event, connection.id)}
        >
          <span className="cm2-tile-icon">
            <i className="ri-computer-line" aria-hidden="true" />
            {authMissing ? (
              <i
                className="ri-error-warning-fill cm2-tile-warn"
                role="img"
                aria-label="私钥认证但没有可用密钥"
              />
            ) : null}
          </span>
          <span className="cm2-tile-name">
            {connection.favorite ? (
              <i className="ri-star-fill cm2-star" aria-hidden="true" />
            ) : null}
            {connection.name}
          </span>
          <span className="cm2-tile-sub">{connection.host}</span>
        </button>
      </Tooltip>
    );
  };

  return (
    <div className="cm2-grid-wrap" ref={wrapRef} onClick={handleBackdropClick}>
      {sections.length === 0 ? (
        <p className="cm2-grid-empty">{emptyText}</p>
      ) : (
        sections.map((section) => (
          <section key={section.key} className="cm2-grid-section">
            {section.title ? <h3 className="cm2-grid-title">{section.title}</h3> : null}
            <div className="cm2-grid">
              {section.items.map((item) =>
                item.kind === "folder" ? renderFolder(item) : renderConnection(item)
              )}
            </div>
          </section>
        ))
      )}
    </div>
  );
};

export const ConnectionGrid = memo(ConnectionGridInner);
