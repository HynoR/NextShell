const EXTENSION_MAP: Record<string, string> = {
  json: "json",
  jsonc: "json",
  yaml: "yaml",
  yml: "yaml",
  toml: "toml",
  py: "python",
  pyw: "python",
  sh: "shell",
  bash: "shell",
  zsh: "shell",
  js: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  ts: "typescript",
  mts: "typescript",
  cts: "typescript",
  jsx: "javascript",
  tsx: "typescript",
  css: "css",
  scss: "scss",
  less: "less",
  html: "html",
  htm: "html",
  xml: "xml",
  svg: "xml",
  md: "markdown",
  markdown: "markdown",
  sql: "sql",
  go: "go",
  rs: "rust",
  java: "java",
  kt: "kotlin",
  rb: "ruby",
  php: "php",
  lua: "lua",
  c: "c",
  h: "c",
  cpp: "cpp",
  cc: "cpp",
  cxx: "cpp",
  hpp: "cpp",
  cs: "csharp",
  swift: "swift",
  r: "r",
  pl: "perl",
  pm: "perl",
  ini: "ini",
  conf: "ini",
  cfg: "ini",
  properties: "ini",
  dockerfile: "dockerfile",
  makefile: "makefile",
  graphql: "graphql",
  gql: "graphql",
};

export const detectLanguage = (filePath: string): string => {
  const fileName = filePath.split("/").pop() ?? "";
  const lower = fileName.toLowerCase();

  if (lower === "dockerfile" || lower.startsWith("dockerfile.")) return "dockerfile";
  if (lower === "makefile" || lower === "gnumakefile") return "makefile";

  const dotIndex = lower.lastIndexOf(".");
  if (dotIndex < 0) return "plaintext";

  const ext = lower.slice(dotIndex + 1);
  return EXTENSION_MAP[ext] ?? "plaintext";
};
