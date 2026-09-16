import { createPortal } from "react-dom";
import { useEffect, useLayoutEffect, useRef, useState } from "react";

export interface PointerMenuItem {
  key: string;
  label: string;
  icon?: string;
  danger?: boolean;
  disabled?: boolean;
  onSelect: () => void;
}

interface PointerMenuProps {
  x: number;
  y: number;
  items: PointerMenuItem[];
  onClose: () => void;
}

const MENU_GAP = 4;
const MENU_MIN_WIDTH = 180;

const resolvePosition = (
  x: number,
  y: number,
  width: number,
  height: number
): { left: number; top: number } => {
  const maxLeft = window.innerWidth - width - MENU_GAP;
  const maxTop = window.innerHeight - height - MENU_GAP;
  const left = Math.min(Math.max(MENU_GAP, x), Math.max(MENU_GAP, maxLeft));
  const below = y + height + MENU_GAP <= window.innerHeight - MENU_GAP;
  const top = below ? y + MENU_GAP : y - height - MENU_GAP;
  return { left, top: Math.min(Math.max(MENU_GAP, top), Math.max(MENU_GAP, maxTop)) };
};

const PointerMenuEntry = ({
  item,
  onSelect
}: {
  item: PointerMenuItem;
  onSelect: (item: PointerMenuItem) => void;
}) => (
  <button
    type="button"
    disabled={item.disabled}
    onClick={() => onSelect(item)}
    style={{
      display: "flex",
      alignItems: "center",
      gap: 8,
      width: "100%",
      padding: "6px 10px",
      border: 0,
      borderRadius: "var(--r-sm)",
      background: "transparent",
      color: item.danger ? "var(--err)" : "var(--t1)",
      font: "inherit",
      textAlign: "left",
      cursor: item.disabled ? "not-allowed" : "pointer",
      opacity: item.disabled ? 0.35 : 1,
      whiteSpace: "nowrap"
    }}
  >
    {item.icon ? <i className={item.icon} aria-hidden="true" /> : null}
    {item.label}
  </button>
);

export const PointerMenu = ({ x, y, items, onClose }: PointerMenuProps) => {
  const menuRef = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState({ left: x, top: y + MENU_GAP });

  useLayoutEffect(() => {
    const menu = menuRef.current;
    if (!menu) return;
    setPosition(resolvePosition(x, y, menu.offsetWidth, menu.offsetHeight));
  }, [items, x, y]);

  useEffect(() => {
    const handlePointerDown = (event: MouseEvent) => {
      if (!menuRef.current?.contains(event.target as Node)) onClose();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("mousedown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("mousedown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [onClose]);

  const selectItem = (item: PointerMenuItem) => {
    onClose();
    item.onSelect();
  };
  const menu = (
    <div
      ref={menuRef}
      role="menu"
      onContextMenu={(event) => event.preventDefault()}
      style={{
        position: "fixed",
        left: position.left,
        top: position.top,
        zIndex: 2000,
        minWidth: MENU_MIN_WIDTH,
        padding: 4,
        background: "var(--bg-elevated)",
        border: "1px solid var(--border-mid)",
        borderRadius: "var(--r-md)",
        boxShadow: "0 8px 32px rgba(0, 0, 0, 0.55), 0 2px 8px rgba(0, 0, 0, 0.3)",
        userSelect: "none"
      }}
    >
      {items.map((item) => (
        <PointerMenuEntry key={item.key} item={item} onSelect={selectItem} />
      ))}
    </div>
  );

  return typeof document === "undefined" ? null : createPortal(menu, document.body);
};
