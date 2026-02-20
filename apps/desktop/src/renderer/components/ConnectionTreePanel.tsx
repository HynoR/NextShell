import { useMemo, useState } from "react";
import type { ConnectionProfile, SessionDescriptor } from "@nextshell/core";

interface ConnectionTreePanelProps {
  connections: ConnectionProfile[];
  sessions: SessionDescriptor[];
  activeConnectionId?: string;
  onSelect: (connectionId: string) => void;
  onConnectByDoubleClick: (connectionId: string) => void;
  onConnect: (connectionId: string) => void;
}

interface GroupNode {
  type: "group";
  key: string;
  label: string;
  children: TreeNode[];
}

interface LeafNode {
  type: "leaf";
  connection: ConnectionProfile;
  isConnected: boolean;
}

type TreeNode = GroupNode | LeafNode;

const buildTree = (
  connections: ConnectionProfile[],
  sessions: SessionDescriptor[],
  keyword: string
): GroupNode => {
  const lower = keyword.trim().toLowerCase();
  const root: GroupNode = {
    type: "group",
    key: "root",
    label: "连接",
    children: []
  };

  const connectedConnectionIds = new Set(
    sessions
      .filter((s) => s.status === "connected" && s.type === "terminal")
      .map((s) => s.connectionId)
  );

  const ensureGroup = (path: string[]): GroupNode => {
    let current = root;
    const visited: string[] = [];

    for (const segment of path) {
      visited.push(segment);
      const key = `group:${visited.join("/")}`;
      let next = current.children.find(
        (node): node is GroupNode => node.type === "group" && node.key === key
      );
      if (!next) {
        next = { type: "group", key, label: segment, children: [] };
        current.children.push(next);
      }
      current = next;
    }

    return current;
  };

  for (const connection of connections) {
    const searchable = `${connection.name} ${connection.host} ${connection.tags.join(" ")} ${connection.notes ?? ""}`.toLowerCase();
    if (lower && !searchable.includes(lower)) {
      continue;
    }

    const group = ensureGroup(connection.groupPath);
    const isConnected = connectedConnectionIds.has(connection.id);
    group.children.push({
      type: "leaf",
      connection,
      isConnected
    });
  }

  return root;
};

/* ── Sub-components ── */

const GroupRow = ({
  node,
  expanded,
  onToggle
}: {
  node: GroupNode;
  expanded: boolean;
  onToggle: () => void;
}) => (
  <button type="button" className="ct-group-row" onClick={onToggle}>
    <i
      className={expanded ? "ri-arrow-down-s-line" : "ri-arrow-right-s-line"}
      aria-hidden="true"
    />
    <i className="ri-folder-3-line ct-group-icon" aria-hidden="true" />
    <span className="ct-group-label">{node.label}</span>
    <span className="ct-group-count">{countLeaves(node)}</span>
  </button>
);

const ServerRow = ({
  node,
  isActive,
  onSelect,
  onDoubleClick,
  onConnect
}: {
  node: LeafNode;
  isActive: boolean;
  onSelect: () => void;
  onDoubleClick: () => void;
  onConnect: () => void;
}) => {
  const c = node.connection;
  return (
    <button
      type="button"
      className={`ct-server-row${isActive ? " active" : ""}${node.isConnected ? " connected" : ""}`}
      onClick={onSelect}
      onDoubleClick={(e) => {
        e.stopPropagation();
        onDoubleClick();
      }}
      title={`${c.name} (${c.host}:${c.port})`}
    >
      <span className={`ct-status-dot${node.isConnected ? " online" : ""}`} />
      {c.favorite ? (
        <i className="ri-star-fill ct-star" aria-hidden="true" />
      ) : null}
      <span className="ct-server-name">{c.name}</span>
      <span className="ct-server-host">{c.host}</span>
      <span
        className="ct-connect-btn"
        title="新建终端连接"
        role="button"
        tabIndex={0}
        onClick={(e) => {
          e.stopPropagation();
          onConnect();
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            e.stopPropagation();
            onConnect();
          }
        }}
      >
        <i className="ri-terminal-box-line" aria-hidden="true" />
      </span>
    </button>
  );
};

function countLeaves(node: GroupNode): number {
  let count = 0;
  for (const child of node.children) {
    if (child.type === "leaf") count += 1;
    else count += countLeaves(child);
  }
  return count;
}

const TreeGroup = ({
  node,
  depth,
  expanded,
  toggleExpanded,
  activeConnectionId,
  onSelect,
  onDoubleClick,
  onConnect
}: {
  node: GroupNode;
  depth: number;
  expanded: Set<string>;
  toggleExpanded: (key: string) => void;
  activeConnectionId?: string;
  onSelect: (id: string) => void;
  onDoubleClick: (id: string) => void;
  onConnect: (id: string) => void;
}) => {
  const isExpanded = expanded.has(node.key);
  return (
    <div className="ct-group" style={{ "--depth": depth } as React.CSSProperties}>
      {depth > 0 && (
        <GroupRow
          node={node}
          expanded={isExpanded}
          onToggle={() => toggleExpanded(node.key)}
        />
      )}
      {(depth === 0 || isExpanded) && (
        <div className="ct-group-children">
          {node.children.map((child) =>
            child.type === "group" ? (
              <TreeGroup
                key={child.key}
                node={child}
                depth={depth + 1}
                expanded={expanded}
                toggleExpanded={toggleExpanded}
                activeConnectionId={activeConnectionId}
                onSelect={onSelect}
                onDoubleClick={onDoubleClick}
                onConnect={onConnect}
              />
            ) : (
              <ServerRow
                key={child.connection.id}
                node={child}
                isActive={child.connection.id === activeConnectionId}
                onSelect={() => onSelect(child.connection.id)}
                onDoubleClick={() => onDoubleClick(child.connection.id)}
                onConnect={() => onConnect(child.connection.id)}
              />
            )
          )}
        </div>
      )}
    </div>
  );
};

export const ConnectionTreePanel = ({
  connections,
  sessions,
  activeConnectionId,
  onSelect,
  onConnectByDoubleClick,
  onConnect
}: ConnectionTreePanelProps) => {
  const [keyword, setKeyword] = useState("");
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set(["root"]));

  const tree = useMemo(
    () => buildTree(connections, sessions, keyword),
    [connections, sessions, keyword]
  );

  const connectedCount = useMemo(
    () =>
      new Set(
        sessions
          .filter((s) => s.status === "connected" && s.type === "terminal")
          .map((s) => s.connectionId)
      ).size,
    [sessions]
  );

  const toggleExpanded = (key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  // Auto-expand all groups when keyword changes
  useMemo(() => {
    if (keyword.trim()) {
      const keys = new Set<string>(["root"]);
      const walk = (node: GroupNode) => {
        keys.add(node.key);
        for (const child of node.children) {
          if (child.type === "group") walk(child);
        }
      };
      walk(tree);
      setExpanded(keys);
    }
  }, [keyword, tree]);

  return (
    <section className="ct-panel">

      {/* Search */}
      <div className="ct-search">
        <i className="ri-search-line ct-search-icon" aria-hidden="true" />
        <input
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
          placeholder="搜索名称 / IP / 标签"
          className="ct-search-input"
        />
        {keyword && (
          <button
            className="ct-search-clear"
            onClick={() => setKeyword("")}
            title="清除搜索"
          >
            <i className="ri-close-line" aria-hidden="true" />
          </button>
        )}
      </div>

      {/* Tree */}
      <div className="ct-tree">
        {tree.children.length === 0 ? (
          <div className="ct-empty">
            {keyword ? (
              <>
                <i className="ri-search-line" aria-hidden="true" />
                <span>未找到匹配的连接</span>
              </>
            ) : (
              <>
                <i className="ri-server-line" aria-hidden="true" />
                <span>暂无连接，点击右上角设置添加</span>
              </>
            )}
          </div>
        ) : (
          <TreeGroup
            node={tree}
            depth={0}
            expanded={expanded}
            toggleExpanded={toggleExpanded}
            activeConnectionId={activeConnectionId}
            onSelect={onSelect}
            onDoubleClick={onConnectByDoubleClick}
            onConnect={onConnect}
          />
        )}
      </div>
    </section>
  );
};
