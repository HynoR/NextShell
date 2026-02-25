import type { LanguageSupport } from "@codemirror/language";
import { StreamLanguage } from "@codemirror/language";
import { javascript } from "@codemirror/lang-javascript";
import { html } from "@codemirror/lang-html";
import { css } from "@codemirror/lang-css";
import { json } from "@codemirror/lang-json";
import { markdown } from "@codemirror/lang-markdown";
import { yaml } from "@codemirror/lang-yaml";
import { python } from "@codemirror/lang-python";
import { php } from "@codemirror/lang-php";
import { toml } from "@codemirror/legacy-modes/mode/toml";
import { shell } from "@codemirror/legacy-modes/mode/shell";

type LangFactory = () => LanguageSupport;

export type EditorSyntaxMode =
  | "auto"
  | "plain"
  | "javascript"
  | "html"
  | "css"
  | "json"
  | "markdown"
  | "yaml"
  | "toml"
  | "python"
  | "shell"
  | "php";

const EXTENSION_MAP: Record<string, LangFactory> = {
  js: () => javascript(),
  mjs: () => javascript(),
  cjs: () => javascript(),
  jsx: () => javascript({ jsx: true }),
  html: () => html(),
  htm: () => html(),
  css: () => css(),
  json: () => json(),
  jsonc: () => json(),
  md: () => markdown(),
  markdown: () => markdown(),
  yaml: () => yaml(),
  yml: () => yaml(),
  toml: () => StreamLanguage.define(toml) as unknown as LanguageSupport,
  py: () => python(),
  pyw: () => python(),
  sh: () => StreamLanguage.define(shell) as unknown as LanguageSupport,
  bash: () => StreamLanguage.define(shell) as unknown as LanguageSupport,
  zsh: () => StreamLanguage.define(shell) as unknown as LanguageSupport,
  php: () => php(),
};

const SYNTAX_MODE_MAP: Record<Exclude<EditorSyntaxMode, "auto" | "plain">, LangFactory> = {
  javascript: () => javascript({ jsx: true }),
  html: () => html(),
  css: () => css(),
  json: () => json(),
  markdown: () => markdown(),
  yaml: () => yaml(),
  toml: () => StreamLanguage.define(toml) as unknown as LanguageSupport,
  python: () => python(),
  shell: () => StreamLanguage.define(shell) as unknown as LanguageSupport,
  php: () => php(),
};

const resolveByExtension = (filePath: string): LanguageSupport | undefined => {
  const fileName = filePath.split("/").pop() ?? "";
  const lower = fileName.toLowerCase();

  if (lower === "makefile" || lower === "gnumakefile") {
    return StreamLanguage.define(shell) as unknown as LanguageSupport;
  }

  const dotIndex = lower.lastIndexOf(".");
  if (dotIndex < 0) return undefined;

  const ext = lower.slice(dotIndex + 1);
  const factory = EXTENSION_MAP[ext];
  return factory?.();
};

export const getLanguageSupport = (
  filePath: string,
  mode: EditorSyntaxMode = "auto"
): LanguageSupport | undefined => {
  if (mode === "plain") {
    return undefined;
  }
  if (mode !== "auto") {
    return SYNTAX_MODE_MAP[mode]?.();
  }
  return resolveByExtension(filePath);
};
