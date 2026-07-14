import path from "node:path";
import { fileURLToPath } from "node:url";

const pathsMatch = (left: string, right: string, platform: NodeJS.Platform): boolean => {
  const leftPath = path.resolve(left);
  const rightPath = path.resolve(right);
  return platform === "win32"
    ? leftPath.toLowerCase() === rightPath.toLowerCase()
    : leftPath === rightPath;
};

export const isTrustedRendererUrl = (
  rawUrl: string,
  appPath: string,
  devServerUrl: string | undefined,
  platform: NodeJS.Platform = process.platform
): boolean => {
  let candidate: URL;
  try {
    candidate = new URL(rawUrl);
  } catch {
    return false;
  }

  if (devServerUrl) {
    try {
      return candidate.origin === new URL(devServerUrl).origin;
    } catch {
      return false;
    }
  }

  if (candidate.protocol !== "file:") {
    return false;
  }
  return pathsMatch(fileURLToPath(candidate), path.join(appPath, "dist/index.html"), platform);
};
