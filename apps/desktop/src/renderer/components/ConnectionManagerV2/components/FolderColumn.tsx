import type { MouseEvent } from "react";
import { Tooltip } from "antd";
import type { ConnectionFolder, ConnectionProfile } from "@nextshell/core";
import { buildBreadcrumb, listChildFolders } from "../utils/folderNavigation";

interface FolderColumnProps {
  rootLabel: string;
  folders: ConnectionFolder[];
  connections: ConnectionProfile[];
  currentFolderId?: string;
  includeSubfolders: boolean;
  onToggleIncludeSubfolders: (value: boolean) => void;
  onEnterFolder: (folderId: string | undefined) => void;
  onCreateFolder: () => void;
  onFolderContextMenu: (event: MouseEvent, folder: ConnectionFolder) => void;
  visibleConnectionCount: number;
}

/**
 * 钻取式目录栏:只渲染当前层。缩进恒为 0,所以再深的路径也不会挤掉名字——这是三栏布局里目录栏
 * 必然偏窄时唯一能保住可读性的做法。全局视野由中栏的「含子目录」承担。
 */
export const FolderColumn = ({
  rootLabel,
  folders,
  connections,
  currentFolderId,
  includeSubfolders,
  onToggleIncludeSubfolders,
  onEnterFolder,
  onCreateFolder,
  onFolderContextMenu,
  visibleConnectionCount
}: FolderColumnProps) => {
  const breadcrumb = buildBreadcrumb(currentFolderId, folders, rootLabel);
  const children = listChildFolders(currentFolderId, folders, connections);

  return (
    <div className="cm2-folder-col">
      <nav className="cm2-breadcrumb" aria-label="目录路径">
        {breadcrumb.map((segment, index) => (
          <span key={segment.folderId ?? "__root__"} className="cm2-breadcrumb-item">
            {index > 0 ? (
              <i className="ri-arrow-right-s-line cm2-breadcrumb-sep" aria-hidden="true" />
            ) : null}
            <button
              type="button"
              className={`cm2-breadcrumb-btn${
                index === breadcrumb.length - 1 ? " cm2-breadcrumb-btn--current" : ""
              }`}
              onClick={() => onEnterFolder(segment.folderId)}
              disabled={index === breadcrumb.length - 1}
            >
              {segment.label}
            </button>
          </span>
        ))}
      </nav>

      <div className="cm2-folder-list">
        {children.length === 0 ? (
          <p className="cm2-folder-empty">此处没有子目录</p>
        ) : (
          children.map((item) => (
            <button
              key={item.folder.id}
              type="button"
              className="cm2-folder-row"
              onClick={() => onEnterFolder(item.folder.id)}
              onContextMenu={(event) => onFolderContextMenu(event, item.folder)}
              title={item.folder.name}
            >
              <i
                className={item.hasChildren ? "ri-folders-line" : "ri-folder-3-line"}
                aria-hidden="true"
              />
              <span className="cm2-folder-name">{item.folder.name}</span>
              <span className="cm2-folder-count">{item.connectionCount}</span>
            </button>
          ))
        )}
      </div>

      <div className="cm2-folder-footer">
        <label className="cm2-subfolder-toggle">
          <input
            type="checkbox"
            checked={includeSubfolders}
            onChange={(event) => onToggleIncludeSubfolders(event.target.checked)}
          />
          含子目录
          <span className="cm2-folder-count">{visibleConnectionCount}</span>
        </label>
        <Tooltip title="在当前目录下新建子目录">
          <button
            type="button"
            className="cm2-icon-btn"
            onClick={onCreateFolder}
            aria-label="新建目录"
          >
            <i className="ri-folder-add-line" aria-hidden="true" />
          </button>
        </Tooltip>
      </div>
    </div>
  );
};
