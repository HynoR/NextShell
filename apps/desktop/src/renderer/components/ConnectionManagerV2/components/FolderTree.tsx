import { useCallback, useMemo, useState } from "react";
import type { Key } from "react";
import { Button, Tooltip, Tree } from "antd";
import type { TreeDataNode, TreeProps } from "antd";
import type { ConnectionFolder, ConnectionProfile } from "@nextshell/core";
import {
  FOLDER_TREE_ROOT_KEY,
  buildFolderTree,
  isSelfOrAncestor,
  type FolderTreeNode
} from "../utils/folderTree";
import { PointerMenu, type PointerMenuItem } from "./PointerMenu";

interface FolderTreeProps {
  rootLabel: string;
  folders: ConnectionFolder[];
  /** 当前作用域的全部连接,用于树上的子树计数。 */
  connections: ConnectionProfile[];
  currentFolderId?: string;
  onSelectFolder: (folderId: string | undefined) => void;
  onCreateFolder: (parentId: string | undefined) => void;
  onRenameFolder: (folder: ConnectionFolder) => void;
  onDeleteFolder: (folder: ConnectionFolder) => void;
  onMoveFolder: (folderId: string, parentId: string | undefined) => void;
}

/**
 * 经典树形目录导航(替换钻取式)。选中某个目录 = 查看该子树的连接;根节点即"全部"。
 * 拖目录到目录上可以改层级;右键提供新建子目录/重命名/删除。
 */
export const FolderTree = ({
  rootLabel,
  folders,
  connections,
  currentFolderId,
  onSelectFolder,
  onCreateFolder,
  onRenameFolder,
  onDeleteFolder,
  onMoveFolder
}: FolderTreeProps) => {
  const [expandedKeys, setExpandedKeys] = useState<Key[]>([FOLDER_TREE_ROOT_KEY]);
  const [menu, setMenu] = useState<{ x: number; y: number; items: PointerMenuItem[] } | null>(
    null
  );

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

  const handleRightClick = useCallback<NonNullable<TreeProps["onRightClick"]>>(
    ({ event, node }) => {
      const key = String(node.key);
      const folder = folderById.get(key);
      const items: PointerMenuItem[] =
        key === FOLDER_TREE_ROOT_KEY || !folder
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
                key: "delete",
                label: "删除目录",
                icon: "ri-delete-bin-line",
                danger: true,
                onSelect: () => onDeleteFolder(folder)
              }
            ];
      setMenu({ x: event.clientX, y: event.clientY, items });
    },
    [folderById, onCreateFolder, onDeleteFolder, onRenameFolder]
  );

  const handleDrop = useCallback<NonNullable<TreeProps["onDrop"]>>(
    (info) => {
      const dragKey = String(info.dragNode.key);
      const dropKey = String(info.node.key);
      if (dragKey === FOLDER_TREE_ROOT_KEY) {
        return;
      }
      // 落在节点上 = 成为其子目录;落在缝隙 = 与该节点同级。
      let parentId: string | undefined;
      if (!info.dropToGap) {
        parentId = dropKey === FOLDER_TREE_ROOT_KEY ? undefined : dropKey;
      } else {
        parentId =
          dropKey === FOLDER_TREE_ROOT_KEY ? undefined : folderById.get(dropKey)?.parentId;
      }
      const dragged = folderById.get(dragKey);
      if (!dragged || (dragged.parentId ?? undefined) === parentId) {
        return;
      }
      if (isSelfOrAncestor(dragKey, parentId, folders)) {
        return;
      }
      onMoveFolder(dragKey, parentId);
    },
    [folderById, folders, onMoveFolder]
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
      <div className="cm2-folder-tree">
        <Tree
          blockNode
          showIcon
          treeData={treeData}
          selectedKeys={[currentFolderId ?? FOLDER_TREE_ROOT_KEY]}
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
