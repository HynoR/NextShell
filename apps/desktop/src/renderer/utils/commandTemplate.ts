import type { ScopedCommandItem } from "@nextshell/core";

const TEMPLATE_PLACEHOLDER_REGEX = /\[#(\w+)\]/g;
const CMD_PARAMS_STORAGE_PREFIX = "nextshell:cmdParams:";

export function extractPlaceholderKeys(command: string): string[] {
  const keys: string[] = [];
  let match: RegExpExecArray | null;
  TEMPLATE_PLACEHOLDER_REGEX.lastIndex = 0;
  while ((match = TEMPLATE_PLACEHOLDER_REGEX.exec(command)) !== null) {
    keys.push(match[1] ?? "");
  }
  return [...new Set(keys)];
}

export function substituteTemplate(
  command: string,
  params: Record<string, string>
): string {
  return command.replace(TEMPLATE_PLACEHOLDER_REGEX, (_, key: string) =>
    params[key] !== undefined && params[key] !== "" ? params[key] : ""
  );
}

export function getCommandStorageKey(command: ScopedCommandItem): string {
  return command.scope === "workspace"
    ? `workspace:${command.workspaceId ?? "unknown"}:${command.id}`
    : `local:${command.id}`;
}

export function loadParamsFromStorage(storageKey: string): Record<string, string> {
  try {
    const raw = localStorage.getItem(CMD_PARAMS_STORAGE_PREFIX + storageKey);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as unknown;
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return Object.fromEntries(
        Object.entries(parsed).filter(
          (entry): entry is [string, string] =>
            typeof entry[0] === "string" && typeof entry[1] === "string"
        )
      );
    }
  } catch {
    // ignore
  }
  return {};
}

export function saveParamsToStorage(
  storageKey: string,
  params: Record<string, string>
): void {
  try {
    localStorage.setItem(
      CMD_PARAMS_STORAGE_PREFIX + storageKey,
      JSON.stringify(params)
    );
  } catch {
    // ignore
  }
}

export function clearParamsFromStorage(storageKey: string): void {
  try {
    localStorage.removeItem(CMD_PARAMS_STORAGE_PREFIX + storageKey);
  } catch {
    // ignore
  }
}
