import type { CloudSyncWorkspaceProfile, ConnectionProfile } from "@nextshell/core";
import {
  CONNECTION_ZONES,
  buildGroupPath,
  isValidZone,
  type ConnectionUpsertInput,
  type ConnectionZone
} from "@nextshell/shared";
import { normalizeGroupPath } from "./tree";

export interface ConnectionFormValues extends ConnectionUpsertInput {
  groupZone?: string;
  groupSubPath?: string;
  workspaceId?: string;
}

export interface ToConnectionPayloadOptions {
  selectedConnectionId?: string;
  generateId?: () => string;
  workspaces?: CloudSyncWorkspaceProfile[];
}

const workspaceRootSlug = (workspaceName: string): string => {
  const normalized = workspaceName
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "");
  return normalized || "workspace";
};

const buildWorkspaceGroupPath = (workspace: CloudSyncWorkspaceProfile, subPath: string): string => {
  const slug = workspaceRootSlug(workspace.workspaceName);
  const normalizedSubPath = subPath.trim().replace(/\\/g, "/").replace(/\/+/g, "/");
  if (!normalizedSubPath || normalizedSubPath === "/") {
    return `/workspace/${slug}`;
  }
  const suffix = normalizedSubPath.startsWith("/") ? normalizedSubPath : `/${normalizedSubPath}`;
  return `/workspace/${slug}${suffix}`;
};

export const sanitizeOptionalText = (value: string | undefined): string | undefined => {
  if (typeof value !== "string") {
    return undefined;
  }

  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : undefined;
};

export const sanitizeTextArray = (values: string[] | undefined): string[] => {
  return (values ?? [])
    .map((item) => item.trim())
    .filter((item) => item.length > 0);
};

export const toQuickUpsertInput = (
  connection: ConnectionProfile,
  patch: Partial<ConnectionUpsertInput>
): ConnectionUpsertInput => ({
  id: connection.id,
  name: connection.name,
  host: connection.host,
  port: connection.port,
  username: connection.username,
  authType: connection.authType,
  sshKeyId: connection.sshKeyId,
  hostFingerprint: connection.hostFingerprint,
  strictHostKeyChecking: connection.strictHostKeyChecking,
  proxyId: connection.proxyId,
  keepAliveEnabled: connection.keepAliveEnabled,
  keepAliveIntervalSec: connection.keepAliveIntervalSec,
  terminalEncoding: connection.terminalEncoding,
  backspaceMode: connection.backspaceMode,
  deleteMode: connection.deleteMode,
  groupPath: connection.groupPath,
  tags: connection.tags,
  notes: connection.notes,
  favorite: connection.favorite,
  monitorSession: connection.monitorSession,
  ...patch
});

export const toConnectionPayload = (
  values: ConnectionFormValues,
  options: ToConnectionPayloadOptions = {}
): ConnectionUpsertInput => {
  const password = sanitizeOptionalText(values.password);
  const hostFingerprint = sanitizeOptionalText(values.hostFingerprint);
  const zone = (
    values.groupZone && isValidZone(values.groupZone) ? values.groupZone : CONNECTION_ZONES.SERVER
  ) as ConnectionZone;
  const subPath = values.groupSubPath ?? "";
  const workspaceId = zone === CONNECTION_ZONES.WORKSPACE ? sanitizeOptionalText(values.workspaceId) : undefined;
  const workspace = workspaceId
    ? options.workspaces?.find((item) => item.id === workspaceId)
    : undefined;
  const groupPath = normalizeGroupPath(
    zone === CONNECTION_ZONES.WORKSPACE && workspace
      ? buildWorkspaceGroupPath(workspace, subPath)
      : buildGroupPath(zone, subPath)
  );
  const tags = sanitizeTextArray(values.tags);
  const notes = sanitizeOptionalText(values.notes);
  const rawPort = values.port as unknown as number | null | undefined;
  const port = rawPort == null ? NaN : Number(rawPort);
  const host = values.host.trim();
  const name = sanitizeOptionalText(values.name) ?? `${host}:${port}`;
  const terminalEncoding = values.terminalEncoding ?? "utf-8";
  const backspaceMode = values.backspaceMode ?? "ascii-backspace";
  const deleteMode = values.deleteMode ?? "vt220-delete";
  const rawKeepAliveInterval = values.keepAliveIntervalSec as unknown as number | null | undefined;
  const keepAliveIntervalSec = rawKeepAliveInterval == null ? undefined : Number(rawKeepAliveInterval);
  const keepAliveEnabled = values.keepAliveEnabled ?? undefined;
  const username = (values.username ?? "").trim();

  return {
    id: values.id ?? options.selectedConnectionId ?? options.generateId?.() ?? crypto.randomUUID(),
    name,
    host,
    port,
    username,
    authType: values.authType,
    password,
    sshKeyId: values.authType === "privateKey" ? values.sshKeyId : undefined,
    hostFingerprint,
    strictHostKeyChecking: values.strictHostKeyChecking ?? false,
    proxyId: values.proxyId,
    keepAliveEnabled,
    keepAliveIntervalSec,
    terminalEncoding,
    backspaceMode,
    deleteMode,
    tags,
    groupPath,
    notes,
    favorite: values.favorite ?? false,
    monitorSession: values.monitorSession ?? false,
    workspaceId
  };
};
