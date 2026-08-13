import { Button, Space } from "antd";

interface BulkBarProps {
  count: number;
  onClear: () => void;
  onBindAuth: () => void;
  onCopyToScope: () => void;
  onExport: () => void;
  onDelete: () => void;
}

/**
 * 勾选后从表格顶部滑出。批量绑定认证在旧管理器里只藏在右键子菜单——那是导入几十台机器之后
 * 最需要的操作,也是最找不到的。
 */
export const BulkBar = ({
  count,
  onClear,
  onBindAuth,
  onCopyToScope,
  onExport,
  onDelete
}: BulkBarProps) => (
  <div className="cm2-bulk-bar" role="toolbar" aria-label="批量操作">
    <span className="cm2-bulk-count">已选 {count}</span>
    <Space size={8} wrap>
      <Button size="small" icon={<i className="ri-key-2-line" aria-hidden="true" />} onClick={onBindAuth}>
        绑定认证
      </Button>
      <Button
        size="small"
        icon={<i className="ri-file-copy-line" aria-hidden="true" />}
        onClick={onCopyToScope}
      >
        复制到作用域…
      </Button>
      <Button
        size="small"
        icon={<i className="ri-download-2-line" aria-hidden="true" />}
        onClick={onExport}
      >
        导出
      </Button>
      <Button
        size="small"
        danger
        icon={<i className="ri-delete-bin-line" aria-hidden="true" />}
        onClick={onDelete}
      >
        删除
      </Button>
      <Button size="small" type="text" onClick={onClear}>
        取消选择
      </Button>
    </Space>
  </div>
);
