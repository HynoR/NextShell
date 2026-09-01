import type { ScopedCommandItem } from "@nextshell/core";

const TEMPLATE_PLACEHOLDER_REGEX = /\[#(\w+)\]/g;
const CMD_PARAMS_STORAGE_PREFIX = "nextshell:cmdParams:";
const PARAM_HISTORY_LIMIT = 10;

export type CommandParamHistory = Record<string, string[]>;

export function extractPlaceholderKeys(command: string): string[] {
  const keys: string[] = [];
  let match: RegExpExecArray | null;
  TEMPLATE_PLACEHOLDER_REGEX.lastIndex = 0;
  while ((match = TEMPLATE_PLACEHOLDER_REGEX.exec(command)) !== null) {
    keys.push(match[1] ?? "");
  }
  return [...new Set(keys)];
}

export function substituteTemplate(command: string, params: Record<string, string>): string {
  return command.replace(TEMPLATE_PLACEHOLDER_REGEX, (_, key: string) => params[key] ?? "");
}

export function getCommandStorageKey(command: ScopedCommandItem): string {
  return command.scope === "workspace"
    ? `workspace:${command.workspaceId ?? "unknown"}:${command.id}`
    : `local:${command.id}`;
}

export function loadParamsFromStorage(storageKey: string): CommandParamHistory {
  try {
    const raw = localStorage.getItem(CMD_PARAMS_STORAGE_PREFIX + storageKey);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {};
    return Object.fromEntries(
      Object.entries(parsed).flatMap(([key, value]) => {
        const values = Array.isArray(value)
          ? value.filter((item): item is string => typeof item === "string")
          : typeof value === "string"
            ? [value]
            : [];
        return values.length > 0 ? [[key, values.slice(0, PARAM_HISTORY_LIMIT)]] : [];
      })
    );
  } catch {
    return {};
  }
}

export function saveParamsToStorage(storageKey: string, params: Record<string, string>): void {
  try {
    const history = loadParamsFromStorage(storageKey);
    for (const [key, value] of Object.entries(params)) {
      if (!value) continue;
      history[key] = [value, ...(history[key] ?? []).filter((item) => item !== value)].slice(
        0,
        PARAM_HISTORY_LIMIT
      );
    }
    localStorage.setItem(CMD_PARAMS_STORAGE_PREFIX + storageKey, JSON.stringify(history));
  } catch {
    // localStorage is optional in private/restricted renderer contexts.
  }
}

export function clearParamsFromStorage(storageKey: string): void {
  try {
    localStorage.removeItem(CMD_PARAMS_STORAGE_PREFIX + storageKey);
  } catch {
    // ignore
  }
}
