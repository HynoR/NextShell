import path from "node:path";

const normalizeAssetPath = (value: string, platform: NodeJS.Platform): string => {
  const pathApi = platform === "win32" ? path.win32 : path.posix;
  let normalized = value;
  if (platform === "win32") {
    normalized = normalized.replace(/^\/(?=[A-Za-z]:[\\/])/, "").replaceAll("/", "\\");
  }
  return pathApi.resolve(normalized);
};

export const resolveAllowedAssetPath = (
  requestUrl: string,
  configuredPath: string | undefined,
  platform: NodeJS.Platform = process.platform
): string | undefined => {
  const allowedValue = configuredPath?.trim();
  if (!allowedValue) {
    return undefined;
  }

  let url: URL;
  try {
    url = new URL(requestUrl);
  } catch {
    return undefined;
  }
  if (url.protocol !== "nextshell-asset:" || url.hostname !== "local") {
    return undefined;
  }

  const requestedPath = normalizeAssetPath(decodeURIComponent(url.pathname), platform);
  const allowedPath = normalizeAssetPath(allowedValue, platform);
  const matches =
    platform === "win32"
      ? requestedPath.toLowerCase() === allowedPath.toLowerCase()
      : requestedPath === allowedPath;
  return matches ? allowedPath : undefined;
};
