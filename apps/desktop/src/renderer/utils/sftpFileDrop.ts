type FileLike = {
  name?: string;
  path?: string;
};

type EntryLike = {
  isFile?: boolean;
  isDirectory?: boolean;
};

type ItemLike = {
  kind?: string;
  getAsFile?: () => FileLike | null;
  webkitGetAsEntry?: () => EntryLike | null;
};

type DataTransferLike = {
  types?: Iterable<string> | ArrayLike<string>;
  files?: ArrayLike<FileLike>;
  items?: ArrayLike<ItemLike>;
};

const hasFilesType = (types: DataTransferLike["types"]): boolean => {
  if (!types) {
    return false;
  }

  for (const type of Array.from(types)) {
    if (type === "Files") {
      return true;
    }
  }

  return false;
};

export const canAcceptSftpFileDrop = (input: {
  active: boolean;
  connected: boolean;
  hasConnection: boolean;
  busy: boolean;
}): boolean => input.active && input.connected && input.hasConnection && !input.busy;

export const isExternalFileDrag = (dataTransfer: DataTransferLike | null | undefined): boolean => {
  if (!dataTransfer) {
    return false;
  }

  if (!hasFilesType(dataTransfer.types)) {
    return false;
  }

  return Array.from(dataTransfer.items ?? []).some((item) => item.kind === "file")
    || Array.from(dataTransfer.files ?? []).length > 0;
};

export interface DropExtractionResult {
  paths: string[];
  /** items existed but all paths were empty — likely a platform/source limitation */
  allPathsEmpty: boolean;
}

export const extractDroppedFilePaths = (dataTransfer: DataTransferLike | null | undefined): DropExtractionResult => {
  if (!dataTransfer) {
    return { paths: [], allPathsEmpty: false };
  }

  const collected: string[] = [];
  const seen = new Set<string>();
  const items = Array.from(dataTransfer.items ?? []);
  let fileItemCount = 0;

  const pushPath = (value: string | undefined): void => {
    const normalized = value?.trim();
    if (!normalized || seen.has(normalized)) {
      return;
    }
    seen.add(normalized);
    collected.push(normalized);
  };

  for (const item of items) {
    if (item.kind !== "file") {
      continue;
    }
    fileItemCount += 1;
    const entry = item.webkitGetAsEntry?.() ?? null;
    if (entry && entry.isFile === false) {
      continue;
    }
    pushPath(item.getAsFile?.()?.path);
  }

  // Fallback to dataTransfer.files only when items were absent
  if (fileItemCount === 0) {
    for (const file of Array.from(dataTransfer.files ?? [])) {
      fileItemCount += 1;
      pushPath(file.path);
    }
  }

  return {
    paths: collected,
    allPathsEmpty: fileItemCount > 0 && collected.length === 0
  };
};
