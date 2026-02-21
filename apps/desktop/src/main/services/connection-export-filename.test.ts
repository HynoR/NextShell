import {
  buildBaseFileName,
  resolveUniqueFileName,
  sanitizeFileName
} from "./connection-export-filename";

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected \"${String(expected)}\", got \"${String(actual)}\"`);
  }
};

(() => {
  assertEqual(
    buildBaseFileName({ name: "prod/web", host: "10.0.0.1:22" }),
    "prod_web-10.0.0.1_22",
    "buildBaseFileName should sanitize invalid characters"
  );
})();

(() => {
  assertEqual(sanitizeFileName(" "), "connection.json", "empty filename should fallback");
  assertEqual(sanitizeFileName("CON"), "CON_.json", "windows reserved names should be suffixed");
  assertEqual(sanitizeFileName("abc. "), "abc.json", "trailing dots/spaces should be trimmed");
})();

(() => {
  const existing = new Set<string>(["srv.json", "srv (2).json"]);
  const resolved = resolveUniqueFileName("srv.json", (candidate) => existing.has(candidate));
  assertEqual(resolved, "srv (3).json", "resolveUniqueFileName should append sequence");
})();
