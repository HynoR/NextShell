import type { languages } from "monaco-editor";
import { monaco } from "../monaco-setup";

export const tomlLanguageDefinition: languages.IMonarchLanguage = {
  tokenizer: {
    root: [
      [/#.*$/, "comment"],
      [/\[\[[\w.\-]+\]\]/, "type.identifier"],
      [/\[[\w.\-]+\]/, "type.identifier"],
      [/[a-zA-Z_][\w.\-]*(?=\s*=)/, "variable"],
      [/=/, "delimiter"],
      [/"([^"\\]|\\.)*"/, "string"],
      [/'[^']*'/, "string"],
      [/"""[\s\S]*?"""/, "string"],
      [/'''[\s\S]*?'''/, "string"],
      [/true|false/, "keyword"],
      [/\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}:\d{2})?/, "number.date"],
      [/-?\d+\.\d+/, "number.float"],
      [/-?\d+/, "number"],
    ]
  }
};

monaco.languages.register({ id: "toml", extensions: [".toml"], aliases: ["TOML"] });
monaco.languages.setMonarchTokensProvider("toml", tomlLanguageDefinition);
