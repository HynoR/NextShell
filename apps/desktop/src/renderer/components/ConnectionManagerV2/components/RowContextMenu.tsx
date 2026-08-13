import { useEffect, useLayoutEffect, useRef, useState } from "react";

export interface ContextMenuItem {
  key: string;
  label: string;
  icon: string;
  danger?: boolean;
  disabled?: boolean;
  onSelect: () => void;
}

interface RowContextMenuProps {
  x: number;
  y: number;
  items: ContextMenuItem[];
  onClose: () => void;
}

/**
 * 行右键菜单。第一项固定是「编辑」——右键是进入编辑态最短的路径,其余命令按选中数量切换成
 * 批量版本。全局命令(导入/导出/排序)不在这里,它们与"你右击的这一行"无关,归工具条。
 */
export const RowContextMenu = ({ x, y, items, onClose }: RowContextMenuProps) => {
  const ref = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState({ left: x, top: y, visible: false });

  // 先渲染再量尺寸：贴着视口右/下边缘打开时要往回收，否则菜单有一半在屏幕外。
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element) {
      return;
    }
    const rect = element.getBoundingClientRect();
    setPosition({
      left: Math.max(8, Math.min(x, window.innerWidth - rect.width - 8)),
      top: Math.max(8, Math.min(y, window.innerHeight - rect.height - 8)),
      visible: true
    });
  }, [x, y]);

  useEffect(() => {
    const dismiss = () => onClose();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      }
    };
    window.addEventListener("mousedown", dismiss);
    window.addEventListener("blur", dismiss);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", dismiss);
      window.removeEventListener("blur", dismiss);
      window.removeEventListener("keydown", onKey);
    };
  }, [onClose]);

  return (
    <div
      ref={ref}
      className="cm2-ctx"
      role="menu"
      style={{
        left: position.left,
        top: position.top,
        visibility: position.visible ? "visible" : "hidden"
      }}
      onMouseDown={(event) => event.stopPropagation()}
      onContextMenu={(event) => event.preventDefault()}
    >
      {items.map((item) => (
        <button
          key={item.key}
          type="button"
          role="menuitem"
          className={`cm2-ctx-item${item.danger ? " cm2-ctx-item--danger" : ""}`}
          disabled={item.disabled}
          onClick={() => {
            onClose();
            item.onSelect();
          }}
        >
          <i className={item.icon} aria-hidden="true" />
          {item.label}
        </button>
      ))}
    </div>
  );
};
