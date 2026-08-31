import { memo } from "react";
import { Select } from "antd";
import type { ConnectionSort } from "../types";
import type { BreadcrumbSegment } from "../utils/folderNavigation";
import { GRID_SORT_OPTIONS, gridSortValue, resolveGridSort } from "../utils/gridItems";

interface GridPathBarProps {
  /** `buildBreadcrumb` 的输出:根段在最前,末段就是当前目录。 */
  segments: BreadcrumbSegment[];
  /** 搜索中面包屑没有意义(搜索无视目录),换成命中数。 */
  searching: boolean;
  /** 「最近连接」视图:面包屑换成固定标签,排序隐藏(顺序就是最近优先)。 */
  recent: boolean;
  resultCount: number;
  sort: ConnectionSort;
  onSortChange: (sort: ConnectionSort) => void;
  onSelectFolder: (folderId: string | undefined) => void;
}

/**
 * 网格上方那条细路径栏。左侧面包屑是钻取式导航的回溯路径(每段可点),右侧是排序——
 * 网格没有表头,排序失去了原来的落脚点,收在这里。
 */
const GridPathBarInner = ({
  segments,
  searching,
  recent,
  resultCount,
  sort,
  onSortChange,
  onSelectFolder
}: GridPathBarProps) => (
  <div className="cm2-pathbar">
    <div className="cm2-pathbar-trail">
      {searching ? (
        <span className="cm2-pathbar-search">搜索结果 {resultCount} 台</span>
      ) : recent ? (
        <span className="cm2-pathbar-search">最近连接 {resultCount} 台</span>
      ) : (
        segments.map((segment, index) => (
          <span key={segment.folderId ?? "__root__"} className="cm2-pathbar-seg">
            {index > 0 ? (
              <i className="ri-arrow-right-s-line cm2-pathbar-sep" aria-hidden="true" />
            ) : null}
            <button
              type="button"
              className={`cm2-pathbar-link${
                index === segments.length - 1 ? " cm2-pathbar-link--current" : ""
              }`}
              onClick={() => onSelectFolder(segment.folderId)}
            >
              {segment.label}
            </button>
          </span>
        ))
      )}
    </div>
    {recent && !searching ? null : (
    <Select
      className="cm2-pathbar-sort"
      size="small"
      variant="borderless"
      value={gridSortValue(sort)}
      onChange={(value) => onSortChange(resolveGridSort(value))}
      options={GRID_SORT_OPTIONS.map((option) => ({
        value: option.value,
        label: option.label
      }))}
      aria-label="排序方式"
    />
    )}
  </div>
);

export const GridPathBar = memo(GridPathBarInner);
