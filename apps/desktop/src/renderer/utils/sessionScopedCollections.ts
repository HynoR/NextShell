export type SessionScopedCollection<T = unknown> = Map<string, T> | Set<string>;

const listCollectionKeys = (collection: SessionScopedCollection): string[] => {
  return Array.from(collection.keys());
};

export const deleteSessionFromCollections = (
  sessionId: string,
  collections: SessionScopedCollection[]
): void => {
  collections.forEach((collection) => {
    collection.delete(sessionId);
  });
};

export const retainSessionsInCollections = (
  sessionIds: Iterable<string>,
  collections: SessionScopedCollection[]
): void => {
  const knownSessionIds = sessionIds instanceof Set ? sessionIds : new Set(sessionIds);

  collections.forEach((collection) => {
    listCollectionKeys(collection).forEach((sessionId) => {
      if (!knownSessionIds.has(sessionId)) {
        collection.delete(sessionId);
      }
    });
  });
};

const trimSessionMapToLimit = <T>(
  collection: Map<string, T>,
  maxEntries: number,
  pinnedSessionIds: Iterable<string>
): void => {
  if (maxEntries <= 0) {
    collection.clear();
    return;
  }

  const pinned = new Set(pinnedSessionIds);
  while (collection.size > maxEntries) {
    let evictionCandidate: string | undefined;
    for (const sessionId of collection.keys()) {
      if (!pinned.has(sessionId)) {
        evictionCandidate = sessionId;
        break;
      }
    }

    if (!evictionCandidate) {
      evictionCandidate = collection.keys().next().value;
    }

    if (!evictionCandidate) {
      break;
    }

    collection.delete(evictionCandidate);
  }
};

export const setBoundedSessionMapEntry = <T>(
  collection: Map<string, T>,
  sessionId: string,
  value: T,
  maxEntries: number,
  pinnedSessionIds: Iterable<string> = []
): void => {
  if (maxEntries <= 0) {
    collection.clear();
    return;
  }

  if (collection.has(sessionId)) {
    collection.delete(sessionId);
  }

  collection.set(sessionId, value);
  trimSessionMapToLimit(collection, maxEntries, pinnedSessionIds);
};
