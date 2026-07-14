import type { ConnectionProfile, SessionDescriptor } from "@nextshell/core";

export const getBatchTargetConnectionIds = (sessions: SessionDescriptor[]): string[] => {
  const seen = new Set<string>();
  const targetIds: string[] = [];
  for (const session of sessions) {
    if (!session.connectionId) {
      continue;
    }
    if (seen.has(session.connectionId)) {
      continue;
    }
    seen.add(session.connectionId);
    targetIds.push(session.connectionId);
  }
  return targetIds;
};

// ─── Batch target tree (grouped connection picker) ───────────────────────────

export interface BatchTargetTreeNode {
  /** Display label. */
  title: string;
  /** A connection id (leaf) or a synthetic `grp:<path>` key (group node). */
  value: string;
  children?: BatchTargetTreeNode[];
}

const GROUP_VALUE_PREFIX = "grp:";
const DEFAULT_GROUP_PATH = "/server";

/**
 * Build a checkable tree of connections grouped by `groupPath` for the batch
 * target selector. Group nodes carry a synthetic `grp:<path>` value (never a
 * real connection id); leaf nodes carry the connection id. Combined with
 * AntD `TreeSelect` + `SHOW_CHILD`, checking a group selects all its
 * connections while the returned value array contains only connection ids.
 */
export const buildBatchTargetTree = (connections: ConnectionProfile[]): BatchTargetTreeNode[] => {
  const roots: BatchTargetTreeNode[] = [];
  const groupByPath = new Map<string, BatchTargetTreeNode>();

  const ensureGroup = (rawPath: string): BatchTargetTreeNode => {
    const segments = (rawPath || DEFAULT_GROUP_PATH).split("/").filter(Boolean);
    if (segments.length === 0) {
      segments.push("server");
    }
    let acc = "";
    let parentChildren = roots;
    let node: BatchTargetTreeNode | undefined;
    for (const segment of segments) {
      acc += `/${segment}`;
      node = groupByPath.get(acc);
      if (!node) {
        node = { title: segment, value: `${GROUP_VALUE_PREFIX}${acc}`, children: [] };
        groupByPath.set(acc, node);
        parentChildren.push(node);
      }
      parentChildren = node.children as BatchTargetTreeNode[];
    }
    return node as BatchTargetTreeNode;
  };

  const sorted = [...connections].sort((a, b) => a.name.localeCompare(b.name));
  for (const connection of sorted) {
    const group = ensureGroup(connection.groupPath);
    (group.children as BatchTargetTreeNode[]).push({
      title: `${connection.name} (${connection.host})`,
      value: connection.id
    });
  }

  return roots;
};
