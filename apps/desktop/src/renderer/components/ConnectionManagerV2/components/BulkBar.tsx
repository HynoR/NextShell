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
    <button type="button" className="cm2-btn" onClick={onBindAuth}>
      <i className="ri-key-2-line" aria-hidden="true" />
      绑定认证
    </button>
    <button type="button" className="cm2-btn" onClick={onCopyToScope}>
      <i className="ri-file-copy-line" aria-hidden="true" />
      复制到作用域…
    </button>
    <button type="button" className="cm2-btn" onClick={onExport}>
      <i className="ri-download-2-line" aria-hidden="true" />
      导出
    </button>
    <button type="button" className="cm2-btn cm2-btn--danger" onClick={onDelete}>
      <i className="ri-delete-bin-line" aria-hidden="true" />
      删除
    </button>
    <button type="button" className="cm2-btn" onClick={onClear} aria-label="取消选择">
      取消选择
    </button>
  </div>
);
