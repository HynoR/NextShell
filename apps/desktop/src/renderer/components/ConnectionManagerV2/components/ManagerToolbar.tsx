import { Dropdown } from "antd";
import type { MenuProps } from "antd";
import {
  CONNECTION_COLUMN_LABELS,
  DEFAULT_CONNECTION_COLUMNS,
  type ConnectionColumnKey
} from "../types";

interface ManagerToolbarProps {
  columns: ConnectionColumnKey[];
  onColumnsChange: (columns: ConnectionColumnKey[]) => void;
  onNewConnection: () => void;
  onImport: () => void;
  onExportAll: () => void;
  onNewFolder: () => void;
}

const ALL_COLUMNS = Object.keys(CONNECTION_COLUMN_LABELS) as ConnectionColumnKey[];

/**
 * 全局命令的唯一落点。旧管理器把导入/导出/排序塞在连接的右键菜单里,和"你右击的那一行"毫无
 * 关系;导入还额外占了一整个 tab,内容只是格式 × 文件/文件夹的 2×2 四个选择器。
 */
export const ManagerToolbar = ({
  columns,
  onColumnsChange,
  onNewConnection,
  onImport,
  onExportAll,
  onNewFolder
}: ManagerToolbarProps) => {
  const columnMenu: MenuProps = {
    selectable: false,
    items: ALL_COLUMNS.map((key) => ({
      key,
      label: (
        <label className="cm2-col-toggle">
          <input
            type="checkbox"
            checked={columns.includes(key)}
            // 名称列不可关：关掉之后表格里没有任何东西能标识一行。
            disabled={key === "name"}
            onChange={(event) => {
              const next = event.target.checked
                ? ALL_COLUMNS.filter((item) => item === key || columns.includes(item))
                : columns.filter((item) => item !== key);
              onColumnsChange(next.length > 0 ? next : DEFAULT_CONNECTION_COLUMNS);
            }}
          />
          {CONNECTION_COLUMN_LABELS[key]}
        </label>
      )
    }))
  };

  return (
    <div className="cm2-toolbar">
      <button type="button" className="cm2-btn cm2-btn--primary" onClick={onNewConnection}>
        <i className="ri-add-line" aria-hidden="true" />
        新建连接
      </button>
      <button type="button" className="cm2-btn" onClick={onNewFolder}>
        <i className="ri-folder-add-line" aria-hidden="true" />
        新建目录
      </button>
      <span className="cm2-toolbar-gap" />
      <button type="button" className="cm2-btn" onClick={onImport}>
        <i className="ri-upload-2-line" aria-hidden="true" />
        导入
      </button>
      <button type="button" className="cm2-btn" onClick={onExportAll}>
        <i className="ri-download-2-line" aria-hidden="true" />
        导出全部
      </button>
      <Dropdown menu={columnMenu} trigger={["click"]} placement="bottomRight">
        <button type="button" className="cm2-btn">
          <i className="ri-layout-column-line" aria-hidden="true" />
          列
        </button>
      </Dropdown>
    </div>
  );
};
