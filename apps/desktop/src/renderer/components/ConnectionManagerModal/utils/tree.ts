import type { CloudSyncWorkspaceProfile, ConnectionProfile } from "@nextshell/core";
import {
  CONNECTION_ZONES,
  ZONE_DISPLAY_NAMES,
  ZONE_ICONS,
  enforceZonePrefix,
  isValidZone,
  type ConnectionZone
} from "@nextshell/shared";
import type { MgrGroupNode, MgrLeafNode } from "../types";

export const MANAGER_SEARCH_RESULT_LIMIT = 400;

const workspaceRootSlug = (workspaceName: string): string => {
  const normalized = workspaceName
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "");
  return normalized || "workspace";
};

interface WorkspaceRootMeta {
  slug: string;
  label: string;
  workspaceId?: string;
}

export interface ManagerConnectionSearchEntry {
  connectionId: string;
  text: string;
}

export interface ManagerTreeBuildResult {
  tree: MgrGroupNode;
  totalMatches: number;
  visibleMatches: number;
  limited: boolean;
}

export const normalizeGroupPath = (value: string | undefined): string => {
  if (!value) return "/server";
  let path = value.trim().replace(/\\/g, "/");
  if (!path.startsWith("/")) path = `/${path}`;
  path = path.replace(/\/+/g, "/");
  if (path.length > 1 && path.endsWith("/")) path = path.slice(0, -1);
  return enforceZonePrefix(path || "/server");
};

export const groupKeyToPath = (key: string): string => {
  if (key === "root") return "/";
  const prefix = "mgr-group:";
  const raw = key.startsWith(prefix) ? key.slice(prefix.length) : key;
  return `/${raw}`;
};

export const groupPathToSegments = (groupPath: string): string[] => {
  return groupPath.split("/").filter((segment) => segment.length > 0);
};

const normalizeSearchText = (value: string): string =>
  value
    .toLowerCase()
    .replace(/[\\/]+/g, " ")
    .replace(/\s+/g, " ")
    .trim();

const tokenizeSearchQuery = (keyword: string): string[] =>
  normalizeSearchText(keyword)
    .split(" ")
    .filter((token) => token.length > 0);

const buildWorkspaceLabelBySlug = (
  workspaces: CloudSyncWorkspaceProfile[]
): Map<string, string> => {
  const labels = new Map<string, string>();
  for (const workspace of workspaces) {
    labels.set(
      workspaceRootSlug(workspace.workspaceName),
      workspace.displayName || workspace.workspaceName
    );
  }
  return labels;
};

const getFolderSearchParts = (
  groupPath: string,
  workspaceLabelBySlug: Map<string, string>
): string[] => {
  const segments = groupPathToSegments(groupPath);
  if (segments.length === 0) {
    return [];
  }
  const rootSegment = segments[0];
  if (!rootSegment) {
    return [];
  }
  if (rootSegment === CONNECTION_ZONES.WORKSPACE) {
    const slug = segments[1];
    const workspaceLabel = slug ? workspaceLabelBySlug.get(slug) : undefined;
    return [
      ...(slug ? [slug] : []),
      ...(workspaceLabel && workspaceLabel !== slug ? [workspaceLabel] : []),
      ...segments.slice(2)
    ];
  }
  return isValidZone(rootSegment) ? segments.slice(1) : segments;
};

export const buildConnectionSearchIndex = (
  connections: ConnectionProfile[],
  workspaces: CloudSyncWorkspaceProfile[] = []
): Map<string, ManagerConnectionSearchEntry> => {
  const workspaceLabelBySlug = buildWorkspaceLabelBySlug(workspaces);
  const index = new Map<string, ManagerConnectionSearchEntry>();
  for (const connection of connections) {
    const text = normalizeSearchText(
      [
        connection.name,
        connection.host,
        ...getFolderSearchParts(connection.groupPath, workspaceLabelBySlug),
        connection.notes ?? ""
      ].join(" ")
    );
    index.set(connection.id, { connectionId: connection.id, text });
  }
  return index;
};

const matchesSearchTokens = (
  connection: ConnectionProfile,
  tokens: string[],
  searchIndex?: Map<string, ManagerConnectionSearchEntry>
): boolean => {
  if (tokens.length === 0) {
    return true;
  }
  const entry = searchIndex?.get(connection.id);
  const text =
    entry?.text ?? buildConnectionSearchIndex([connection]).get(connection.id)?.text ?? "";
  return tokens.every((token) => text.includes(token));
};

const assignLeafCounts = (node: MgrGroupNode): number => {
  let count = 0;
  for (const child of node.children) {
    count += child.type === "leaf" ? 1 : assignLeafCounts(child);
  }
  node.leafCount = count;
  return count;
};

export const buildManagerTreeResult = (
  connections: ConnectionProfile[],
  keyword: string,
  workspaces: CloudSyncWorkspaceProfile[] = [],
  emptyFolders?: string[],
  searchIndex?: Map<string, ManagerConnectionSearchEntry>,
  resultLimit = MANAGER_SEARCH_RESULT_LIMIT
): ManagerTreeBuildResult => {
  const tokens = tokenizeSearchQuery(keyword);
  const hasSearch = tokens.length > 0;
  const effectiveSearchIndex =
    searchIndex ?? (hasSearch ? buildConnectionSearchIndex(connections, workspaces) : undefined);
  const root: MgrGroupNode = { type: "group", key: "root", label: "全部连接", children: [] };

  const zoneNodes = new Map<string, MgrGroupNode>();
  for (const zone of [CONNECTION_ZONES.SERVER, CONNECTION_ZONES.IMPORT] as const) {
    const node: MgrGroupNode = {
      type: "group",
      key: `mgr-group:${zone}`,
      label: ZONE_DISPLAY_NAMES[zone],
      children: [],
      zone,
      icon: ZONE_ICONS[zone]
    };
    zoneNodes.set(zone, node);
    root.children.push(node);
  }

  const workspaceRoots = new Map<string, MgrGroupNode>();
  const workspaceMetas = new Map<string, WorkspaceRootMeta>();
  const rememberWorkspaceMeta = (slug: string, meta?: Partial<WorkspaceRootMeta>) => {
    const existing = workspaceMetas.get(slug);
    workspaceMetas.set(slug, {
      slug,
      label: meta?.label ?? existing?.label ?? slug,
      workspaceId: meta?.workspaceId ?? existing?.workspaceId
    });
  };

  for (const workspace of workspaces) {
    const slug = workspaceRootSlug(workspace.workspaceName);
    rememberWorkspaceMeta(slug, {
      label: workspace.displayName || workspace.workspaceName,
      workspaceId: workspace.id
    });
  }

  const rememberWorkspaceSlugFromPath = (groupPath: string) => {
    const segments = groupPathToSegments(groupPath);
    if ((segments[0] ?? "") !== CONNECTION_ZONES.WORKSPACE) {
      return;
    }
    const slug = segments[1];
    if (slug) {
      rememberWorkspaceMeta(slug);
    }
  };

  for (const connection of connections) {
    rememberWorkspaceSlugFromPath(connection.groupPath);
  }
  for (const folderPath of emptyFolders ?? []) {
    rememberWorkspaceSlugFromPath(folderPath);
  }

  const ensureWorkspaceRoot = (slug: string): MgrGroupNode => {
    let node = workspaceRoots.get(slug);
    if (node) {
      return node;
    }
    const meta = workspaceMetas.get(slug) ?? { slug, label: slug };
    node = {
      type: "group",
      key: `mgr-group:${CONNECTION_ZONES.WORKSPACE}/${slug}`,
      label: meta.label,
      children: [],
      zone: CONNECTION_ZONES.WORKSPACE,
      icon: "ri-git-repository-line",
      workspaceId: meta.workspaceId
    };
    workspaceRoots.set(slug, node);
    return node;
  };

  for (const meta of Array.from(workspaceMetas.values()).sort((a, b) =>
    a.label.localeCompare(b.label)
  )) {
    root.children.splice(root.children.length - 1, 0, ensureWorkspaceRoot(meta.slug));
  }

  const ensureGroup = (zoneNode: MgrGroupNode, subSegments: string[]): MgrGroupNode => {
    let pointer = zoneNode;
    const segments: string[] =
      zoneNode.zone === CONNECTION_ZONES.WORKSPACE
        ? [CONNECTION_ZONES.WORKSPACE, zoneNode.key.replace("mgr-group:workspace/", "")]
        : [zoneNode.zone as ConnectionZone];
    for (const part of subSegments) {
      segments.push(part);
      const key = `mgr-group:${segments.join("/")}`;
      let next = pointer.children.find(
        (node): node is MgrGroupNode => node.type === "group" && node.key === key
      );
      if (!next) {
        next = { type: "group", key, label: part, children: [] };
        pointer.children.push(next);
      }
      pointer = next;
    }
    return pointer;
  };

  let totalMatches = 0;
  let visibleMatches = 0;

  for (const connection of connections) {
    if (!matchesSearchTokens(connection, tokens, effectiveSearchIndex)) continue;
    totalMatches += 1;
    if (hasSearch && visibleMatches >= resultLimit) continue;
    visibleMatches += 1;

    const segments = groupPathToSegments(connection.groupPath);
    const zoneName = segments[0] ?? CONNECTION_ZONES.SERVER;
    const zone = isValidZone(zoneName) ? zoneName : CONNECTION_ZONES.SERVER;
    const zoneNode =
      zone === CONNECTION_ZONES.WORKSPACE
        ? ensureWorkspaceRoot(segments[1] ?? "workspace")
        : zoneNodes.get(zone);
    if (!zoneNode) continue;
    const subSegments =
      zone === CONNECTION_ZONES.WORKSPACE
        ? segments.slice(2)
        : isValidZone(zoneName)
          ? segments.slice(1)
          : segments;

    ensureGroup(zoneNode, subSegments).children.push({ type: "leaf", connection });
  }

  if (emptyFolders && !hasSearch) {
    for (const folderPath of emptyFolders) {
      const segments = groupPathToSegments(folderPath);
      const zoneName = segments[0] ?? CONNECTION_ZONES.SERVER;
      const zone = isValidZone(zoneName) ? zoneName : CONNECTION_ZONES.SERVER;
      const zoneNode =
        zone === CONNECTION_ZONES.WORKSPACE
          ? ensureWorkspaceRoot(segments[1] ?? "workspace")
          : zoneNodes.get(zone);
      if (!zoneNode) continue;
      const subSegments =
        zone === CONNECTION_ZONES.WORKSPACE
          ? segments.slice(2)
          : isValidZone(zoneName)
            ? segments.slice(1)
            : segments;
      ensureGroup(zoneNode, subSegments);
    }
  }

  assignLeafCounts(root);
  return {
    tree: root,
    totalMatches: hasSearch ? totalMatches : connections.length,
    visibleMatches: hasSearch ? visibleMatches : connections.length,
    limited: hasSearch && totalMatches > visibleMatches
  };
};

export const buildManagerTree = (
  connections: ConnectionProfile[],
  keyword: string,
  workspaces: CloudSyncWorkspaceProfile[] = [],
  emptyFolders?: string[],
  searchIndex?: Map<string, ManagerConnectionSearchEntry>,
  resultLimit = MANAGER_SEARCH_RESULT_LIMIT
): MgrGroupNode => {
  return buildManagerTreeResult(
    connections,
    keyword,
    workspaces,
    emptyFolders,
    searchIndex,
    resultLimit
  ).tree;
};

export const collectFlatLeafIds = (
  node: MgrGroupNode,
  expandedKeys: Set<string>,
  depth: number
): string[] => {
  const ids: string[] = [];
  if (depth > 0 && !expandedKeys.has(node.key)) return ids;
  for (const child of node.children) {
    if (child.type === "leaf") ids.push(child.connection.id);
    else ids.push(...collectFlatLeafIds(child, expandedKeys, depth + 1));
  }
  return ids;
};

export const collectGroupLeafIds = (node: MgrGroupNode): string[] => {
  const ids: string[] = [];
  for (const child of node.children) {
    if (child.type === "leaf") ids.push(child.connection.id);
    else ids.push(...collectGroupLeafIds(child));
  }
  return ids;
};

export const sortMgrChildren = (
  node: MgrGroupNode,
  mode: "name" | "host" | "createdAt"
): MgrGroupNode => {
  const groups: MgrGroupNode[] = [];
  const leaves: MgrLeafNode[] = [];
  for (const child of node.children) {
    if (child.type === "group") groups.push(sortMgrChildren(child, mode));
    else leaves.push(child);
  }
  if (!node.zone) {
    groups.sort((a, b) => {
      if (a.zone && b.zone) return 0;
      return a.label.localeCompare(b.label);
    });
  }
  leaves.sort((a, b) => {
    if (mode === "host") return a.connection.host.localeCompare(b.connection.host);
    if (mode === "createdAt") {
      return (
        new Date(a.connection.createdAt).getTime() - new Date(b.connection.createdAt).getTime()
      );
    }
    return a.connection.name.localeCompare(b.connection.name);
  });
  return { ...node, children: [...groups, ...leaves] };
};

export const countMgrLeaves = (node: MgrGroupNode): number => {
  if (typeof node.leafCount === "number") {
    return node.leafCount;
  }
  let count = 0;
  for (const child of node.children) {
    if (child.type === "leaf") count += 1;
    else count += countMgrLeaves(child);
  }
  return count;
};
