const INVALID_FILENAME_CHAR_REGEX = /[<>:"/\\|?*\x00-\x1F]/g;
const TRAILING_DOTS_SPACES_REGEX = /[. ]+$/g;

const WINDOWS_RESERVED_BASE_NAMES = new Set([
  "CON", "PRN", "AUX", "NUL",
  "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
  "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"
]);

const normalizeBaseName = (raw: string): string => {
  const replaced = raw.replace(INVALID_FILENAME_CHAR_REGEX, "_").trim();
  const withoutTrailingDotsAndSpaces = replaced.replace(TRAILING_DOTS_SPACES_REGEX, "");

  if (!withoutTrailingDotsAndSpaces) {
    return "connection";
  }

  const upper = withoutTrailingDotsAndSpaces.toUpperCase();
  if (WINDOWS_RESERVED_BASE_NAMES.has(upper)) {
    return `${withoutTrailingDotsAndSpaces}_`;
  }

  return withoutTrailingDotsAndSpaces;
};

export const buildBaseFileName = (input: { name: string; host: string }): string => {
  return normalizeBaseName(`${input.name}-${input.host}`);
};

export const sanitizeFileName = (raw: string): string => {
  const normalized = normalizeBaseName(raw);
  return `${normalized}.json`;
};

export const resolveUniqueFileName = (
  fileName: string,
  exists: (candidate: string) => boolean
): string => {
  if (!exists(fileName)) {
    return fileName;
  }

  const dotIndex = fileName.lastIndexOf(".");
  const hasExt = dotIndex > 0;
  const baseName = hasExt ? fileName.slice(0, dotIndex) : fileName;
  const ext = hasExt ? fileName.slice(dotIndex) : "";

  let index = 2;
  while (true) {
    const candidate = `${baseName} (${index})${ext}`;
    if (!exists(candidate)) {
      return candidate;
    }
    index += 1;
  }
};
