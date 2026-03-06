import {
  persistWorkspacePanelState,
  resolveWorkspacePanelState
} from "./workspaceLayoutState";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const createStorage = (): Storage => {
  const store = new Map<string, string>();

  return {
    get length() {
      return store.size;
    },
    clear() {
      store.clear();
    },
    getItem(key) {
      return store.get(key) ?? null;
    },
    key(index) {
      return Array.from(store.keys())[index] ?? null;
    },
    removeItem(key) {
      store.delete(key);
    },
    setItem(key, value) {
      store.set(key, value);
    }
  };
};

(() => {
  const storage = createStorage();

  assert(
    resolveWorkspacePanelState(storage, "nextshell.workspace.leftSidebarCollapsed", true) === true,
    "resolveWorkspacePanelState should fall back to settings default when storage is empty"
  );
})();

(() => {
  const storage = createStorage();
  storage.setItem("nextshell.workspace.bottomWorkbenchCollapsed", "false");

  assert(
    resolveWorkspacePanelState(storage, "nextshell.workspace.bottomWorkbenchCollapsed", true) === false,
    "resolveWorkspacePanelState should prefer persisted state over settings default"
  );
})();

(() => {
  const storage = createStorage();
  storage.setItem("nextshell.workspace.leftSidebarCollapsed", "invalid");

  assert(
    resolveWorkspacePanelState(storage, "nextshell.workspace.leftSidebarCollapsed", false) === false,
    "resolveWorkspacePanelState should ignore invalid persisted values"
  );
})();

(() => {
  const storage = createStorage();
  persistWorkspacePanelState(storage, "nextshell.workspace.bottomWorkbenchCollapsed", true);

  assert(
    storage.getItem("nextshell.workspace.bottomWorkbenchCollapsed") === "true",
    "persistWorkspacePanelState should serialize boolean values"
  );
})();
