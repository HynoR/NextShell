import { Button, Checkbox, Dropdown, Input } from "antd";
import type { MenuProps } from "antd";
import {
  CONNECTION_COLUMN_LABELS,
  DEFAULT_CONNECTION_COLUMNS,
  type ConnectionColumnKey
} from "../types";

interface ManagerToolbarProps {
  keyword: string;
  onKeywordChange: (keyword: string) => void;
  columns: ConnectionColumnKey[];
  onColumnsChange: (columns: ConnectionColumnKey[]) => void;
  onNewConnection: () => void;
  onImport: () => void;
  onExportAll: () => void;
}

const ALL_COLUMNS = Object.keys(CONNECTION_COLUMN_LABELS) as ConnectionColumnKey[];

/**
 * 全局命令 + 搜索,单行。旧版搜索、工具条各占一行,加上批量条最多三层 chrome 叠在表格上;
 * 目录创建归目录树,这里只留与"整个列表"相关的命令。
 */
export const ManagerToolbar = ({
  keyword,
  onKeywordChange,
  columns,
  onColumnsChange,
  onNewConnection,
  onImport,
  onExportAll
}: ManagerToolbarProps) => {
  const columnMenu: MenuProps = {
    selectable: false,
    items: ALL_COLUMNS.map((key) => ({
      key,
      label: (
        <Checkbox
          checked={columns.includes(key)}
          // 名称列不可关：关掉之后表格里没有任何东西能标识一行。
          disabled={key === "name"}
          onChange={(event) => {
            const next = event.target.checked
              ? ALL_COLUMNS.filter((item) => item === key || columns.includes(item))
              : columns.filter((item) => item !== key);
            onColumnsChange(next.length > 0 ? next : DEFAULT_CONNECTION_COLUMNS);
          }}
        >
          {CONNECTION_COLUMN_LABELS[key]}
        </Checkbox>
      )
    }))
  };

  return (
    <div className="cm2-toolbar">
      <Button
        type="primary"
        icon={<i className="ri-add-line" aria-hidden="true" />}
        onClick={onNewConnection}
      >
        新建连接
      </Button>
      <Input
        className="cm2-search"
        allowClear
        prefix={<i className="ri-search-line cm2-search-icon" aria-hidden="true" />}
        placeholder="搜索整个作用域：名称、地址、用户名、标签、备注"
        value={keyword}
        onChange={(event) => onKeywordChange(event.target.value)}
      />
      <Button icon={<i className="ri-upload-2-line" aria-hidden="true" />} onClick={onImport}>
        导入
      </Button>
      <Button
        icon={<i className="ri-download-2-line" aria-hidden="true" />}
        onClick={onExportAll}
      >
        导出全部
      </Button>
      <Dropdown menu={columnMenu} trigger={["click"]} placement="bottomRight">
        <Button icon={<i className="ri-layout-column-line" aria-hidden="true" />} aria-label="列设置" />
      </Dropdown>
    </div>
  );
};
