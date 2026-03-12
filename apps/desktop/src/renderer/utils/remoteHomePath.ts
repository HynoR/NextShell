const FALLBACK_REMOTE_PATH = "/";

export const resolveInitialRemotePath = async (
  fetcher: () => Promise<{ path: string } | null>
): Promise<string> => {
  try {
    const resolved = await fetcher();
    if (resolved?.path?.startsWith("/")) {
      return resolved.path;
    }
  } catch {
    // Ignore home lookup failures and fall back to the remote root.
  }

  return FALLBACK_REMOTE_PATH;
};
