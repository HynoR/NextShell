import {
  appPreferencesPatchSchema,
  appPreferencesSchema
} from "./contracts";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

(() => {
  const parsed = appPreferencesSchema.safeParse({
    terminal: {
      backgroundColor: "#000000",
      foregroundColor: "#d8eaff",
      fontSize: 14,
      lineHeight: 1.2
    }
  });

  assert(parsed.success, "appPreferencesSchema should accept terminal preferences without explicit fontFamily");
  if (!parsed.success) {
    return;
  }
  assert(
    parsed.data.terminal.fontFamily === "JetBrains Mono, Menlo, Monaco, monospace",
    "appPreferencesSchema should inject default terminal fontFamily"
  );
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    terminal: {
      fontFamily: "\"Fira Code\", monospace"
    }
  });

  assert(parsed.success, "appPreferencesPatchSchema should accept non-empty fontFamily");
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    terminal: {
      fontFamily: "   "
    }
  });

  assert(parsed.success === false, "appPreferencesPatchSchema should reject blank fontFamily");
})();

(() => {
  const parsed = appPreferencesSchema.safeParse({
    terminal: {
      backgroundColor: "#000000",
      foregroundColor: "#d8eaff",
      fontSize: 14,
      lineHeight: 1.2,
      localShell: {
        mode: "preset",
        preset: "system",
        customPath: ""
      }
    }
  });

  assert(parsed.success, "appPreferencesSchema should accept terminal localShell preferences");
  if (!parsed.success) {
    return;
  }
  assert(
    parsed.data.terminal.localShell.mode === "preset",
    "appPreferencesSchema should keep localShell mode"
  );
  assert(
    parsed.data.terminal.localShell.preset === "system",
    "appPreferencesSchema should keep localShell preset"
  );
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    terminal: {
      localShell: {
        mode: "custom",
        preset: "system",
        customPath: "   "
      }
    }
  });

  assert(
    parsed.success === false,
    "appPreferencesPatchSchema should reject blank custom local shell path"
  );
})();

(() => {
  const parsed = appPreferencesPatchSchema.safeParse({
    terminal: {
      localShell: {
        mode: "custom",
        preset: "system",
        customPath: "/bin/fish"
      }
    }
  });

  assert(parsed.success, "appPreferencesPatchSchema should accept custom local shell path");
})();
