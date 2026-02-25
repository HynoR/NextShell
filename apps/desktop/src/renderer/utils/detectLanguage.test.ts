import { getLanguageSupport } from "./detectLanguage";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

(() => {
  const js = getLanguageSupport("/tmp/sample.js");
  assert(Boolean(js), "auto mode should detect js by extension");
})();

(() => {
  const forcedJson = getLanguageSupport("/tmp/noext", "json");
  assert(Boolean(forcedJson), "forced mode should work without extension");
})();

(() => {
  const forcedPlain = getLanguageSupport("/tmp/sample.ts", "plain");
  assert(!forcedPlain, "plain mode should disable syntax highlight");
})();

(() => {
  const forcedShell = getLanguageSupport("/tmp/sample.py", "shell");
  assert(Boolean(forcedShell), "forced mode should override auto-detected extension");
})();
