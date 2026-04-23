import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import type { AppPreferences, ConnectionListQuery, ConnectionProfile } from "@nextshell/core";
import { EncryptedSecretVault } from "@nextshell/security";
import type { SshConnectOptions } from "@nextshell/ssh";
import {
  SQLiteConnectionRepository,
  SQLiteProxyRepository,
  SQLiteSshKeyRepository
} from "@nextshell/storage";

const DEFAULT_PRODUCT_NAME = "NextShell";
const DEFAULT_DB_FILE_NAME = "nextshell.db";
const DEFAULT_SEARCH_LIMIT = 20;

export interface NextShellDataPaths {
  dataDir: string;
  dbPath: string;
}

export interface ResolveNextShellDataPathsOptions {
  dataDir?: string;
  dbPath?: string;
  env?: NodeJS.ProcessEnv;
  homeDir?: string;
  platform?: NodeJS.Platform;
  productName?: string;
}

export interface ServerSummary {
  nameId: string;
  name: string;
  host: string;
  port: number;
  groupPath: string;
  tags: string[];
  favorite: boolean;
}

export interface ResolvedConnectionTarget {
  connection: ConnectionProfile;
  summary: ServerSummary;
}

export interface ReadonlyCredentialContext {
  paths: NextShellDataPaths;
  connections: SQLiteConnectionRepository;
  sshKeys: SQLiteSshKeyRepository;
  proxies: SQLiteProxyRepository;
  vault: EncryptedSecretVault;
  preferences: AppPreferences;
  close: () => void;
}

export class NextShellDataNotFoundError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "NextShellDataNotFoundError";
  }
}

export class CredentialStoreUnavailableError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "CredentialStoreUnavailableError";
  }
}

export class ConnectionTargetNotFoundError extends Error {
  readonly target: string;

  constructor(target: string) {
    super(`No connection matched target: ${target}`);
    this.name = "ConnectionTargetNotFoundError";
    this.target = target;
  }
}

export class ConnectionTargetAmbiguousError extends Error {
  readonly target: string;
  readonly candidates: ServerSummary[];

  constructor(target: string, candidates: ServerSummary[]) {
    super(`Target is ambiguous: ${target}`);
    this.name = "ConnectionTargetAmbiguousError";
    this.target = target;
    this.candidates = candidates;
  }
}

const normalizeText = (value: string): string => value.trim().toLowerCase();

const slugify = (value: string): string => {
  const normalized = value
    .normalize("NFKD")
    .toLowerCase()
    .trim()
    .replace(/[^\p{Letter}\p{Number}]+/gu, "-")
    .replace(/^-+|-+$/g, "");

  return normalized.length > 0 ? normalized : "server";
};

const toStableShortId = (value: string): string => {
  const normalized = value.toLowerCase().replace(/[^a-z0-9]/g, "");
  if (normalized.length >= 8) {
    return normalized.slice(-8);
  }
  return normalized.padStart(8, "0");
};

const getDefaultDataDir = (options: ResolveNextShellDataPathsOptions): string => {
  const env = options.env ?? process.env;
  const homeDir = options.homeDir ?? os.homedir();
  const platform = options.platform ?? process.platform;
  const productName = options.productName ?? DEFAULT_PRODUCT_NAME;

  if (platform === "darwin") {
    return path.join(homeDir, "Library", "Application Support", productName, "storage");
  }

  if (platform === "win32") {
    return path.join(env.APPDATA ?? path.join(homeDir, "AppData", "Roaming"), productName, "storage");
  }

  return path.join(env.XDG_CONFIG_HOME ?? path.join(homeDir, ".config"), productName, "storage");
};

const isHexString = (value: string): boolean => value.length > 0 && value.length % 2 === 0 && /^[0-9a-fA-F]+$/.test(value);

const getSearchableText = (connection: ConnectionProfile): string => {
  return [
    connection.name,
    connection.host,
    connection.groupPath,
    ...connection.tags
  ].join(" ").toLowerCase();
};

const selectUniqueMatches = (connections: ConnectionProfile[]): ConnectionProfile[] => {
  const seen = new Set<string>();
  const matches: ConnectionProfile[] = [];
  for (const connection of connections) {
    if (seen.has(connection.id)) {
      continue;
    }
    seen.add(connection.id);
    matches.push(connection);
  }
  return matches;
};

const resolveMatches = (
  connections: ConnectionProfile[],
  target: string,
  predicate: (connection: ConnectionProfile, normalizedTarget: string) => boolean
): ConnectionProfile[] => {
  const normalizedTarget = normalizeText(target);
  return selectUniqueMatches(connections.filter((connection) => predicate(connection, normalizedTarget)));
};

const toSummaryMap = (connections: ConnectionProfile[]): ServerSummary[] => {
  return connections.map((connection) => buildServerSummary(connection));
};

export const resolveNextShellDataPaths = (options: ResolveNextShellDataPathsOptions = {}): NextShellDataPaths => {
  const env = options.env ?? process.env;

  if (options.dbPath ?? env["NEXTSHELL_DB_PATH"]) {
    const dbPath = path.resolve(options.dbPath ?? env["NEXTSHELL_DB_PATH"] ?? "");
    return {
      dataDir: path.dirname(dbPath),
      dbPath
    };
  }

  const dataDir = path.resolve(options.dataDir ?? env["NEXTSHELL_DATA_DIR"] ?? getDefaultDataDir(options));
  return {
    dataDir,
    dbPath: path.join(dataDir, DEFAULT_DB_FILE_NAME)
  };
};

export const createReadonlyCredentialContext = (
  options: ResolveNextShellDataPathsOptions = {}
): ReadonlyCredentialContext => {
  const paths = resolveNextShellDataPaths(options);
  if (!fs.existsSync(paths.dbPath)) {
    throw new NextShellDataNotFoundError(`NextShell data not found at ${paths.dbPath}`);
  }

  const connections = new SQLiteConnectionRepository(paths.dbPath, {
    readonly: true,
    fileMustExist: true
  });
  const sshKeys = new SQLiteSshKeyRepository(connections.getDb());
  const proxies = new SQLiteProxyRepository(connections.getDb());

  const deviceKeyHex = connections.getDeviceKey();
  if (!deviceKeyHex || !isHexString(deviceKeyHex)) {
    connections.close();
    throw new CredentialStoreUnavailableError("credential store unavailable: device key missing or invalid");
  }

  const preferences = connections.getAppPreferences();
  const vault = new EncryptedSecretVault(connections.getSecretStore(), Buffer.from(deviceKeyHex, "hex"));

  return {
    paths,
    connections,
    sshKeys,
    proxies,
    vault,
    preferences,
    close: () => {
      connections.close();
    }
  };
};

export const buildServerSummary = (connection: ConnectionProfile): ServerSummary => {
  const stableId = toStableShortId(connection.resourceId ?? connection.id);
  return {
    nameId: `${slugify(connection.name)}--${stableId}`,
    name: connection.name,
    host: connection.host,
    port: connection.port,
    groupPath: connection.groupPath,
    tags: [...connection.tags],
    favorite: connection.favorite
  };
};

export const listServerSummaries = (
  context: Pick<ReadonlyCredentialContext, "connections">,
  query: ConnectionListQuery = {}
): ServerSummary[] => {
  return context.connections.list(query).map((connection) => buildServerSummary(connection));
};

export const searchServerSummaries = (
  context: Pick<ReadonlyCredentialContext, "connections">,
  query: string,
  limit = DEFAULT_SEARCH_LIMIT
): ServerSummary[] => {
  const normalized = normalizeText(query);
  if (!normalized) {
    return listServerSummaries(context, {}).slice(0, limit);
  }

  return context.connections
    .list({})
    .filter((connection) => getSearchableText(connection).includes(normalized))
    .slice(0, limit)
    .map((connection) => buildServerSummary(connection));
};

export const resolveConnectionTarget = (
  context: Pick<ReadonlyCredentialContext, "connections">,
  target: string
): ResolvedConnectionTarget => {
  const trimmedTarget = target.trim();
  if (!trimmedTarget) {
    throw new ConnectionTargetNotFoundError(target);
  }

  const connections = context.connections.list({});
  const exactNameId = resolveMatches(
    connections,
    trimmedTarget,
    (connection, normalizedTarget) => normalizeText(buildServerSummary(connection).nameId) === normalizedTarget
  );
  if (exactNameId.length === 1) {
    const match = exactNameId[0]!;
    return { connection: match, summary: buildServerSummary(match) };
  }
  if (exactNameId.length > 1) {
    throw new ConnectionTargetAmbiguousError(trimmedTarget, toSummaryMap(exactNameId));
  }

  const exactName = resolveMatches(
    connections,
    trimmedTarget,
    (connection, normalizedTarget) => normalizeText(connection.name) === normalizedTarget
  );
  if (exactName.length === 1) {
    const match = exactName[0]!;
    return { connection: match, summary: buildServerSummary(match) };
  }
  if (exactName.length > 1) {
    throw new ConnectionTargetAmbiguousError(trimmedTarget, toSummaryMap(exactName));
  }

  const exactHost = resolveMatches(
    connections,
    trimmedTarget,
    (connection, normalizedTarget) => normalizeText(connection.host) === normalizedTarget
  );
  if (exactHost.length === 1) {
    const match = exactHost[0]!;
    return { connection: match, summary: buildServerSummary(match) };
  }
  if (exactHost.length > 1) {
    throw new ConnectionTargetAmbiguousError(trimmedTarget, toSummaryMap(exactHost));
  }

  const prefixMatches = resolveMatches(
    connections,
    trimmedTarget,
    (connection, normalizedTarget) => normalizeText(connection.name).startsWith(normalizedTarget)
  );
  if (prefixMatches.length === 1) {
    const match = prefixMatches[0]!;
    return { connection: match, summary: buildServerSummary(match) };
  }
  if (prefixMatches.length > 1) {
    throw new ConnectionTargetAmbiguousError(trimmedTarget, toSummaryMap(prefixMatches));
  }

  const fuzzyMatches = resolveMatches(
    connections,
    trimmedTarget,
    (connection, normalizedTarget) => getSearchableText(connection).includes(normalizedTarget)
  );
  if (fuzzyMatches.length === 1) {
    const match = fuzzyMatches[0]!;
    return { connection: match, summary: buildServerSummary(match) };
  }
  if (fuzzyMatches.length > 1) {
    throw new ConnectionTargetAmbiguousError(trimmedTarget, toSummaryMap(fuzzyMatches));
  }

  throw new ConnectionTargetNotFoundError(trimmedTarget);
};

export const buildSshConnectOptions = async (
  context: Pick<ReadonlyCredentialContext, "preferences" | "proxies" | "sshKeys" | "vault">,
  profile: ConnectionProfile
): Promise<SshConnectOptions> => {
  let proxy: SshConnectOptions["proxy"];
  if (profile.proxyId) {
    const proxyProfile = context.proxies.getById(profile.proxyId);
    if (!proxyProfile) {
      throw new Error("Referenced proxy profile not found");
    }
    const proxySecret = proxyProfile.credentialRef
      ? await context.vault.readCredential(proxyProfile.credentialRef)
      : undefined;
    proxy = {
      type: proxyProfile.proxyType,
      host: proxyProfile.host,
      port: proxyProfile.port,
      username: proxyProfile.username,
      password: proxyProfile.proxyType === "socks5" ? proxySecret : undefined
    };
  }

  const username = profile.username.trim();
  if (!username) {
    throw new Error("SSH username is required");
  }

  const keepAliveEnabled = profile.keepAliveEnabled ?? context.preferences.ssh.keepAliveEnabled;
  const intervalCandidate = profile.keepAliveIntervalSec ?? context.preferences.ssh.keepAliveIntervalSec;
  const keepAliveIntervalSec =
    Number.isInteger(intervalCandidate) && intervalCandidate >= 5 && intervalCandidate <= 600
      ? intervalCandidate
      : context.preferences.ssh.keepAliveIntervalSec;

  const base: Omit<SshConnectOptions, "authType"> = {
    host: profile.host,
    port: profile.port,
    username,
    hostFingerprint: profile.hostFingerprint,
    strictHostKeyChecking: profile.strictHostKeyChecking,
    proxy,
    keepaliveInterval: keepAliveEnabled ? keepAliveIntervalSec * 1000 : 0
  };

  const secret = profile.credentialRef ? await context.vault.readCredential(profile.credentialRef) : undefined;

  if (profile.authType === "password" || profile.authType === "interactive") {
    if (!secret) {
      throw new Error("Password credential is missing");
    }
    return {
      ...base,
      authType: profile.authType,
      password: secret
    };
  }

  if (profile.authType === "privateKey") {
    if (!profile.sshKeyId) {
      throw new Error("Private key auth requires sshKeyId");
    }
    const keyProfile = context.sshKeys.getById(profile.sshKeyId);
    if (!keyProfile) {
      throw new Error("Referenced SSH key not found");
    }
    const privateKey = await context.vault.readCredential(keyProfile.keyContentRef);
    if (!privateKey) {
      throw new Error("Private key content is missing");
    }
    const passphrase = keyProfile.passphraseRef
      ? await context.vault.readCredential(keyProfile.passphraseRef)
      : undefined;
    return {
      ...base,
      authType: "privateKey",
      privateKey,
      passphrase
    };
  }

  return {
    ...base,
    authType: "agent"
  };
};
