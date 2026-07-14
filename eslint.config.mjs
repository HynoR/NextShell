import js from "@eslint/js";
import eslintConfigPrettier from "eslint-config-prettier";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    ignores: [
      "**/node_modules/**",
      "**/dist/**",
      "**/dist-electron/**",
      "**/release/**",
      "**/coverage/**",
      "**/*.tsbuildinfo",
      ".claude/**",
      "docs/**",
      "IPC_OPTIMIZATION_PLAN.md"
    ]
  },
  {
    ...js.configs.recommended,
    files: ["**/*.{js,mjs,cjs,ts,tsx}"],
    languageOptions: {
      globals: {
        ...globals.es2026,
        ...globals.browser,
        ...globals.node,
        ...globals.bunBuiltin
      }
    }
  },
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    plugins: {
      "react-hooks": reactHooks
    },
    rules: {
      "no-constant-binary-expression": "warn",
      "no-control-regex": "off",
      "no-useless-assignment": "warn",
      "no-useless-catch": "warn",
      "no-useless-escape": "warn",
      "prefer-const": "warn",
      "preserve-caught-error": "off",
      "@typescript-eslint/no-explicit-any": "off",
      "@typescript-eslint/no-require-imports": "off",
      "@typescript-eslint/no-unused-vars": [
        "warn",
        {
          argsIgnorePattern: "^_",
          caughtErrorsIgnorePattern: "^_",
          varsIgnorePattern: "^_"
        }
      ],
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "warn"
    }
  },
  eslintConfigPrettier
);
