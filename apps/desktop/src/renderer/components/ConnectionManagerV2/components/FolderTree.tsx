import { memo, useCallback, useMemo, useState } from "react";
import type { DragEvent, Key } from "react";
import { Button, Tooltip, Tree } from "antd";
import type { TreeDataNode, TreeProps } from "antd";
import type { ConnectionFolder, ConnectionProfile } from "@nextshell/core";
import {
  FOLDER_TREE_ROOT_KEY,
  buildFolderTree,
  isSelfOrAncestor,
  type FolderTreeNode
} from "../utils/folderTree";
import {
  CONNECTION_DRAG_MIME,
  isInternalConnectionDrag,
  parseConnectionDragIds
} from "../utils/managerDrop";
import { PointerMenu, type PointerMenuItem } from "./PointerMenu";

interface FolderTreeProps {
  rootLabel: string;
  folders: ConnectionFolder[];
  /** 当前作用域的全部连接,用于树上的子树计数。 */
  connections: ConnectionProfile[];
  currentFolderId?: string;
  onSelectFolder: (folderId: string | undefined) => void;
  /** 中栏当前是「最近连接」视图——树上的选中高亮要让位给顶部的固定入口。 */
  recentActive: boolean;
  onSelectRecent: () => void;
  onCreateFolder: (parentId: string | undefined) => void;
  onRenameFolder: (folder: ConnectionFolder) => void;
  onDeleteFolder: (folder: ConnectionFolder) => void;
  onMoveFolder: (folderId: string, parentId: string | undefined) => void;
  /** 同级排序:把 dragId 放到 dropId 的前/后。 */
  onReorderFolder: (dragId: string, dropId: string, placeAfter: boolean) => void;
  /** 把一批连接拖到某个目录(根节点为 undefined = 顶层)。 */
  onDropConnections: (connectionIds: string[], folderId: string | undefined) => void;
  /** 对该目录递归下的全部连接批量绑定认证。 */
  onBatchBindAuth: (folder: ConnectionFolder) => void;
}

/**
 * 经典树形目录导航(D9′ 追认)。它仍是主导航:点一个目录 = 中栏网格钻到那一层。
 * 拖目录到目录上改层级、拖到缝隙里改同级顺序;拖连接磁贴到节点上 = 移动连接。
 *
 * 「含子目录」开关随平铺视图一并废止(D18):网格是钻取式的,"当前这一层"是它唯一的口径。
 */
const FolderTreeInner = ({
  rootLabel,
  folders,
  connections,
  recentActive,
  onSelectRecent,
  currentFolderId,
  onSelectFolder,
  onCreateFolder,
  onRenameFolder,
  onDeleteFolder,
  onMoveFolder,
  onReorderFolder,
  onDropConnections,
  onBatchBindAuth
}: FolderTreeProps) => {
  const [expandedKeys, setExpandedKeys] = useState<Key[]>([FOLDER_TREE_ROOT_KEY]);
  const [dropKey, setDropKey] = useState<string>();
  const [menu, setMenu] = useState<{ x: number; y: number; items: PointerMenuItem[] } | null>(null);

  const folderById = useMemo(
    () => new Map(folders.map((folder) => [folder.id, folder])),
    [folders]
  );

  const treeData = useMemo<TreeDataNode[]>(() => {
    const toNode = (node: FolderTreeNode): TreeDataNode => ({
      key: node.folder.id,
      title: (
        <span className="cm2-tree-title">
          <span className="cm2-tree-name">{node.folder.name}</span>
          {node.count > 0 ? <span className="cm2-tree-count">{node.count}</span> : null}
        </span>
      ),
      icon: <i className="ri-folder-3-line" aria-hidden="true" />,
      children: node.children.map(toNode)
    });
    const roots = buildFolderTree(folders, connections);
    return [
      {
        key: FOLDER_TREE_ROOT_KEY,
        icon: <i className="ri-computer-line" aria-hidden="true" />,
        title: (
          <span className="cm2-tree-title">
            <span className="cm2-tree-name">{rootLabel}</span>
            <span className="cm2-tree-count">{connections.length}</span>
          </span>
        ),
        children: roots.map(toNode)
      }
    ];
  }, [connections, folders, rootLabel]);

  const handleSelect = useCallback<NonNullable<TreeProps["onSelect"]>>(
    (_keys, info) => {
      const key = String(info.node.key);
      onSelectFolder(key === FOLDER_TREE_ROOT_KEY ? undefined : key);
    },
    [onSelectFolder]
  );

  const buildMenuItems = useCallback(
    (folder: ConnectionFolder | undefined): PointerMenuItem[] =>
      !folder
        ? [
            {
              key: "create",
              label: "新建目录",
              icon: "ri-folder-add-line",
              onSelect: () => onCreateFolder(undefined)
            }
          ]
        : [
            {
              key: "create",
              label: "新建子目录",
              icon: "ri-folder-add-line",
              onSelect: () => onCreateFolder(folder.id)
            },
            {
              key: "rename",
              label: "重命名",
              icon: "ri-pencil-line",
              onSelect: () => onRenameFolder(folder)
            },
            {
              key: "bindAuth",
              label: "批量绑定认证（含子目录）",
              icon: "ri-key-2-line",
              onSelect: () => onBatchBindAuth(folder)
            },
            {
              key: "delete",
              label: "删除目录",
              icon: "ri-delete-bin-line",
              danger: true,
              onSelect: () => onDeleteFolder(folder)
            }
          ],
    [onBatchBindAuth, onCreateFolder, onDeleteFolder, onRenameFolder]
  );

  const handleRightClick = useCallback<NonNullable<TreeProps["onRightClick"]>>(
    ({ event, node }) => {
      const key = String(node.key);
      setMenu({
        x: event.clientX,
        y: event.clientY,
        items: buildMenuItems(key === FOLDER_TREE_ROOT_KEY ? undefined : folderById.get(key))
      });
    },
    [buildMenuItems, folderById]
  );

  const handleDrop = useCallback<NonNullable<TreeProps["onDrop"]>>(
    (info) => {
      const dragKey = String(info.dragNode.key);
      const dropNodeKey = String(info.node.key);
      if (dragKey === FOLDER_TREE_ROOT_KEY) {
        return;
      }
      const dragged = folderById.get(dragKey);
      if (!dragged) {
        return;
      }

      // 落在节点上 = 成为其子目录。
      if (!info.dropToGap) {
        const parentId = dropNodeKey === FOLDER_TREE_ROOT_KEY ? undefined : dropNodeKey;
        if ((dragged.parentId ?? undefined) === parentId) {
          return;
        }
        if (isSelfOrAncestor(dragKey, parentId, folders)) {
          return;
        }
        onMoveFolder(dragKey, parentId);
        return;
      }

      // 落在缝隙 = 与该节点同级并排在它前/后。根节点没有同级，退回"移到顶层"。
      if (dropNodeKey === FOLDER_TREE_ROOT_KEY) {
        if ((dragged.parentId ?? undefined) !== undefined) {
          onMoveFolder(dragKey, undefined);
        }
        return;
      }
      if (isSelfOrAncestor(dragKey, dropNodeKey, folders)) {
        return;
      }
      // antd 给的是绝对位置,减去落点节点自身的下标才知道是"前"还是"后"。
      const positions = info.node.pos.split("-");
      const selfIndex = Number(positions[positions.length - 1] ?? 0);
      onReorderFolder(dragKey, dropNodeKey, info.dropPosition - selfIndex > 0);
    },
    [folderById, folders, onMoveFolder, onReorderFolder]
  );

  const handleConnectionDragOver = useCallback((event: DragEvent<HTMLElement>, key: string) => {
    if (!isInternalConnectionDrag(event.dataTransfer)) {
      return;
    }
    // 只拦自己的拖拽类型:目录自身的拖拽和文件拖入都继续交给上层处理。
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer.dropEffect = "move";
    setDropKey(key);
  }, []);

  const handleConnectionDrop = useCallback(
    (event: DragEvent<HTMLElement>, key: string) => {
      setDropKey(undefined);
      if (!isInternalConnectionDrag(event.dataTransfer)) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      const ids = parseConnectionDragIds(event.dataTransfer.getData(CONNECTION_DRAG_MIME));
      if (ids.length > 0) {
        onDropConnections(ids, key === FOLDER_TREE_ROOT_KEY ? undefined : key);
      }
    },
    [onDropConnections]
  );

  const titleRender = useCallback<NonNullable<TreeProps["titleRender"]>>(
    (node) => {
      const key = String(node.key);
      // treeData 里的 title 都是元素，但类型上还允许函数形态，两种都接住。
      const title = typeof node.title === "function" ? node.title(node) : node.title;
      return (
        <div
          className={`cm2-tree-drop${key === dropKey ? " cm2-tree-drop--active" : ""}`}
          onDragOver={(event) => handleConnectionDragOver(event, key)}
          onDragLeave={() => setDropKey((previous) => (previous === key ? undefined : previous))}
          onDrop={(event) => handleConnectionDrop(event, key)}
        >
          {title}
        </div>
      );
    },
    [dropKey, handleConnectionDragOver, handleConnectionDrop]
  );

  return (
    <div className="cm2-folder-col">
      <div className="cm2-folder-head">
        <span className="cm2-folder-head-label">目录</span>
        <Tooltip title="在当前目录下新建子目录">
          <Button
            type="text"
            size="small"
            icon={<i className="ri-folder-add-line" aria-hidden="true" />}
            onClick={() => onCreateFolder(currentFolderId)}
            aria-label="新建目录"
          />
        </Tooltip>
      </div>
      <button
        type="button"
        className={`cm2-recent-entry${recentActive ? " cm2-recent-entry--active" : ""}`}
        onClick={onSelectRecent}
      >
        <i className="ri-time-line" aria-hidden="true" />
        最近连接
      </button>
      <div className="cm2-folder-tree">
        <Tree
          blockNode
          showIcon
          treeData={treeData}
          titleRender={titleRender}
          selectedKeys={recentActive ? [] : [currentFolderId ?? FOLDER_TREE_ROOT_KEY]}
          expandedKeys={expandedKeys}
          onExpand={setExpandedKeys}
          onSelect={handleSelect}
          onRightClick={handleRightClick}
          draggable={{ icon: false, nodeDraggable: (node) => node.key !== FOLDER_TREE_ROOT_KEY }}
          onDrop={handleDrop}
        />
      </div>
      {menu ? (
        <PointerMenu x={menu.x} y={menu.y} items={menu.items} onClose={() => setMenu(null)} />
      ) : null}
    </div>
  );
};

// 目录树在管理器里每次渲染都要重建整棵 treeData;它跟搜索框、选中态毫无关系,
// 不包 memo 的话每敲一个字都会连带重画一遍。
export const FolderTree = memo(FolderTreeInner);
