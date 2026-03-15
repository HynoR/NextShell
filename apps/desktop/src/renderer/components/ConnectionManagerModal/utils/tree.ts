import type { ConnectionProfile } from "@nextshell/core";
import {
  CONNECTION_ZONES,
  ZONE_DISPLAY_NAMES,
  ZONE_ICONS,
  ZONE_ORDER,
  enforceZonePrefix,
  isValidZone,
  type ConnectionZone
} from "@nextshell/shared";
import type { MgrGroupNode, MgrLeafNode } from "../types";

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

export const buildManagerTree = (
  connections: ConnectionProfile[],
  keyword: string,
  emptyFolders?: string[]
): MgrGroupNode => {
  const lower = keyword.toLowerCase().trim();
  const root: MgrGroupNode = { type: "group", key: "root", label: "全部连接", children: [] };

  const zoneNodes = new Map<string, MgrGroupNode>();
  for (const zone of ZONE_ORDER) {
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

  const ensureGroup = (zoneNode: MgrGroupNode, subSegments: string[]): MgrGroupNode => {
    let pointer = zoneNode;
    const segments: string[] = [zoneNode.zone as ConnectionZone];
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

  for (const connection of connections) {
    const text = `${connection.name} ${connection.host} ${connection.groupPath} ${connection.tags.join(" ")}`.toLowerCase();
    if (lower && !text.includes(lower)) continue;

    const segments = groupPathToSegments(connection.groupPath);
    const zoneName = segments[0] ?? CONNECTION_ZONES.SERVER;
    const zone = isValidZone(zoneName) ? zoneName : CONNECTION_ZONES.SERVER;
    const zoneNode = zoneNodes.get(zone);
    if (!zoneNode) continue;
    const subSegments = isValidZone(zoneName) ? segments.slice(1) : segments;

    ensureGroup(zoneNode, subSegments).children.push({ type: "leaf", connection });
  }

  if (emptyFolders) {
    for (const folderPath of emptyFolders) {
      const segments = groupPathToSegments(folderPath);
      const zoneName = segments[0] ?? CONNECTION_ZONES.SERVER;
      const zone = isValidZone(zoneName) ? zoneName : CONNECTION_ZONES.SERVER;
      const zoneNode = zoneNodes.get(zone);
      if (!zoneNode) continue;
      const subSegments = isValidZone(zoneName) ? segments.slice(1) : segments;
      ensureGroup(zoneNode, subSegments);
    }
  }

  return root;
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

export const sortMgrChildren = (node: MgrGroupNode, mode: "name" | "host" | "createdAt"): MgrGroupNode => {
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
      return new Date(a.connection.createdAt).getTime() - new Date(b.connection.createdAt).getTime();
    }
    return a.connection.name.localeCompare(b.connection.name);
  });
  return { ...node, children: [...groups, ...leaves] };
};

export const countMgrLeaves = (node: MgrGroupNode): number => {
  let count = 0;
  for (const child of node.children) {
    if (child.type === "leaf") count += 1;
    else count += countMgrLeaves(child);
  }
  return count;
};
