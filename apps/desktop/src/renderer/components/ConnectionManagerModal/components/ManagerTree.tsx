import { useRef } from "react";
import type { KeyboardEvent, MouseEvent, ReactNode } from "react";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors
} from "@dnd-kit/core";
import type { DragEndEvent, DragStartEvent } from "@dnd-kit/core";
import type { ConnectionProfile } from "@nextshell/core";
import { countMgrLeaves } from "../utils/tree";
import type { MgrGroupNode } from "../types";

const GroupRow = ({
  node,
  expanded,
  onToggle,
  onContextMenu,
  onCtrlClick
}: {
  node: MgrGroupNode;
  expanded: boolean;
  onToggle: () => void;
  onContextMenu?: (event: MouseEvent) => void;
  onCtrlClick?: () => void;
}) => {
  const { isOver, setNodeRef } = useDroppable({ id: node.key });

  const handleClick = (event: MouseEvent) => {
    if ((event.metaKey || event.ctrlKey) && onCtrlClick) {
      event.preventDefault();
      onCtrlClick();
      return;
    }
    onToggle();
  };

  return (
    <button
      ref={setNodeRef}
      type="button"
      className={`mgr-group-row${isOver ? " mgr-group-row--drop-target" : ""}`}
      onClick={handleClick}
      onContextMenu={onContextMenu}
    >
      <i
        className={expanded ? "ri-arrow-down-s-line" : "ri-arrow-right-s-line"}
        aria-hidden="true"
      />
      <i
        className={node.icon ? node.icon : isOver ? "ri-folder-open-line" : "ri-folder-3-line"}
        aria-hidden="true"
      />
      <span className="mgr-group-label">{node.label}</span>
      <span className="mgr-group-count">{countMgrLeaves(node)}</span>
    </button>
  );
};

const ServerRow = ({
  connection,
  isPrimary,
  isMultiSelected,
  isCutPending,
  isRenaming,
  onSelect,
  onDoubleClick,
  onQuickConnect,
  onContextMenu,
  onRenameCommit,
  onRenameCancel
}: {
  connection: ConnectionProfile;
  isPrimary: boolean;
  isMultiSelected: boolean;
  isCutPending: boolean;
  isRenaming: boolean;
  onSelect: (event: MouseEvent) => void;
  onDoubleClick: () => void;
  onQuickConnect: () => void;
  onContextMenu: (event: MouseEvent) => void;
  onRenameCommit: (newName: string) => void;
  onRenameCancel: () => void;
}) => {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: connection.id,
    data: { connection }
  });
  const renameRef = useRef<HTMLInputElement>(null);

  const handleRenameKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter") {
      const value = renameRef.current?.value.trim();
      onRenameCommit(value || connection.name);
    } else if (event.key === "Escape") {
      onRenameCancel();
    }
  };

  const handleRenameBlur = () => {
    const value = renameRef.current?.value.trim();
    onRenameCommit(value || connection.name);
  };

  return (
    <div
      ref={setNodeRef}
      className={`mgr-server-row${isPrimary ? " selected" : ""}${isMultiSelected ? " multi-selected" : ""}${isDragging ? " mgr-server-row--dragging" : ""}${isCutPending ? " cut-pending" : ""}`}
      {...attributes}
      {...listeners}
      onContextMenu={onContextMenu}
    >
      <button
        type="button"
        className="mgr-server-select-btn"
        onClick={onSelect}
        onDoubleClick={onDoubleClick}
        title={`${connection.name} (${connection.host}:${connection.port})`}
      >
        <span className="mgr-server-status" />
        {connection.favorite ? (
          <i className="ri-star-fill mgr-server-star" aria-hidden="true" />
        ) : null}
        {isRenaming ? (
          <input
            ref={renameRef}
            className="mgr-server-rename-input"
            defaultValue={connection.name}
            autoFocus
            onKeyDown={handleRenameKeyDown}
            onBlur={handleRenameBlur}
            onClick={(event) => event.stopPropagation()}
          />
        ) : (
          <span className="mgr-server-name">{connection.name}</span>
        )}
        {connection.originKind === "cloud" ? (
          <i className="ri-cloud-line" aria-hidden="true" style={{ fontSize: 12, color: "var(--text-tertiary)", marginLeft: 4 }} />
        ) : null}
        {!isRenaming ? <span className="mgr-server-host">{connection.host}</span> : null}
      </button>
      <button
        type="button"
        className="mgr-quick-connect-btn"
        onClick={onQuickConnect}
        title="快速连接"
        aria-label="快速连接"
      >
        <i className="ri-terminal-box-line" aria-hidden="true" />
      </button>
    </div>
  );
};

const TreeGroup = ({
  node,
  depth,
  expanded,
  toggleExpanded,
  primarySelectedId,
  selectedIds,
  cutIds,
  renamingId,
  onSelect,
  onDoubleClick,
  onQuickConnect,
  onContextMenu,
  onGroupContextMenu,
  onGroupCtrlClick,
  onRenameCommit,
  onRenameCancel
}: {
  node: MgrGroupNode;
  depth: number;
  expanded: Set<string>;
  toggleExpanded: (key: string) => void;
  primarySelectedId?: string;
  selectedIds: Set<string>;
  cutIds: Set<string>;
  renamingId?: string;
  onSelect: (id: string, event: MouseEvent) => void;
  onDoubleClick: (connectionId: string) => void;
  onQuickConnect: (connectionId: string) => void;
  onContextMenu: (event: MouseEvent, connectionId: string) => void;
  onGroupContextMenu: (event: MouseEvent, node: MgrGroupNode) => void;
  onGroupCtrlClick: (node: MgrGroupNode) => void;
  onRenameCommit: (connectionId: string, newName: string) => void;
  onRenameCancel: () => void;
}) => {
  const isExpanded = expanded.has(node.key);

  return (
    <div className="mgr-group">
      {depth > 0 ? (
        <GroupRow
          node={node}
          expanded={isExpanded}
          onToggle={() => toggleExpanded(node.key)}
          onContextMenu={(event) => onGroupContextMenu(event, node)}
          onCtrlClick={() => onGroupCtrlClick(node)}
        />
      ) : null}
      {(depth === 0 || isExpanded) ? (
        <div className={`mgr-group-children${depth > 0 ? " mgr-group-children--indented" : ""}`}>
          {node.children.map((child) => child.type === "group" ? (
            <TreeGroup
              key={child.key}
              node={child}
              depth={depth + 1}
              expanded={expanded}
              toggleExpanded={toggleExpanded}
              primarySelectedId={primarySelectedId}
              selectedIds={selectedIds}
              cutIds={cutIds}
              renamingId={renamingId}
              onSelect={onSelect}
              onDoubleClick={onDoubleClick}
              onQuickConnect={onQuickConnect}
              onContextMenu={onContextMenu}
              onGroupContextMenu={onGroupContextMenu}
              onGroupCtrlClick={onGroupCtrlClick}
              onRenameCommit={onRenameCommit}
              onRenameCancel={onRenameCancel}
            />
          ) : (
            <ServerRow
              key={child.connection.id}
              connection={child.connection}
              isPrimary={child.connection.id === primarySelectedId}
              isMultiSelected={selectedIds.has(child.connection.id)}
              isCutPending={cutIds.has(child.connection.id)}
              isRenaming={renamingId === child.connection.id}
              onSelect={(event) => onSelect(child.connection.id, event)}
              onDoubleClick={() => onDoubleClick(child.connection.id)}
              onQuickConnect={() => onQuickConnect(child.connection.id)}
              onContextMenu={(event) => onContextMenu(event, child.connection.id)}
              onRenameCommit={(newName) => onRenameCommit(child.connection.id, newName)}
              onRenameCancel={onRenameCancel}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
};

const RootDropZone = ({
  children,
  onContextMenu
}: {
  children: ReactNode;
  onContextMenu?: (event: MouseEvent) => void;
}) => {
  const { isOver, setNodeRef } = useDroppable({ id: "root" });

  return (
    <div
      ref={setNodeRef}
      className={`mgr-tree-wrap${isOver ? " mgr-tree-wrap--drop-target" : ""}`}
      onContextMenu={onContextMenu}
    >
      {children}
    </div>
  );
};

interface ManagerTreeProps {
  tree: MgrGroupNode;
  expanded: Set<string>;
  primarySelectedId?: string;
  selectedIds: Set<string>;
  cutIds: Set<string>;
  renamingId?: string;
  keyword: string;
  hasVisibleConnections: boolean;
  draggingConnection: ConnectionProfile | null;
  toggleExpanded: (key: string) => void;
  onSelect: (id: string, event: MouseEvent) => void;
  onDoubleClick: (connectionId: string) => void;
  onQuickConnect: (connectionId: string) => void;
  onConnectionContextMenu: (event: MouseEvent, connectionId: string) => void;
  onGroupContextMenu: (event: MouseEvent, node: MgrGroupNode) => void;
  onGroupCtrlClick: (node: MgrGroupNode) => void;
  onRenameCommit: (connectionId: string, newName: string) => void;
  onRenameCancel: () => void;
  onEmptyContextMenu: (event: MouseEvent) => void;
  onDragStart: (event: DragStartEvent) => void;
  onDragEnd: (event: DragEndEvent) => void | Promise<void>;
}

export const ManagerTree = ({
  tree,
  expanded,
  primarySelectedId,
  selectedIds,
  cutIds,
  renamingId,
  keyword,
  hasVisibleConnections,
  draggingConnection,
  toggleExpanded,
  onSelect,
  onDoubleClick,
  onQuickConnect,
  onConnectionContextMenu,
  onGroupContextMenu,
  onGroupCtrlClick,
  onRenameCommit,
  onRenameCancel,
  onEmptyContextMenu,
  onDragStart,
  onDragEnd
}: ManagerTreeProps) => {
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));

  return (
    <DndContext
      sensors={sensors}
      onDragStart={onDragStart}
      onDragEnd={(event) => void onDragEnd(event)}
    >
      <RootDropZone onContextMenu={onEmptyContextMenu}>
        {!hasVisibleConnections ? (
          <div className="mgr-tree-empty">
            {keyword ? (
              <>
                <i className="ri-search-line" aria-hidden="true" />
                <span>未找到匹配连接</span>
              </>
            ) : (
              <>
                <i className="ri-server-line" aria-hidden="true" />
                <span>暂无连接</span>
              </>
            )}
          </div>
        ) : (
          <TreeGroup
            node={tree}
            depth={0}
            expanded={expanded}
            toggleExpanded={toggleExpanded}
            primarySelectedId={primarySelectedId}
            selectedIds={selectedIds}
            cutIds={cutIds}
            renamingId={renamingId}
            onSelect={onSelect}
            onDoubleClick={onDoubleClick}
            onQuickConnect={onQuickConnect}
            onContextMenu={onConnectionContextMenu}
            onGroupContextMenu={onGroupContextMenu}
            onGroupCtrlClick={onGroupCtrlClick}
            onRenameCommit={onRenameCommit}
            onRenameCancel={onRenameCancel}
          />
        )}
      </RootDropZone>
      <DragOverlay>
        {draggingConnection ? (
          <div className="mgr-drag-overlay">
            <i className="ri-server-line" aria-hidden="true" />
            <span>{draggingConnection.name}</span>
            {selectedIds.has(draggingConnection.id) && selectedIds.size > 1 ? (
              <span className="mgr-drag-badge">+{selectedIds.size - 1}</span>
            ) : null}
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
};
