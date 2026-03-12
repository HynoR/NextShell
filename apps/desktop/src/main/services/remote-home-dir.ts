const REMOTE_HOME_DIR_COMMAND = "cd ~ >/dev/null 2>&1 && pwd";

export const buildRemoteHomeDirCommand = (): string => REMOTE_HOME_DIR_COMMAND;

export const parseRemoteHomeDir = (stdout: string): string | null => {
  const candidate = stdout
    .split(/\r?\n/)
    .map((line) => line.trim())
    .find((line) => line.length > 0);

  if (!candidate || !candidate.startsWith("/")) {
    return null;
  }

  return candidate;
};
