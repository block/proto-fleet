import eslint from "@eslint/js";
import { fixupPluginRules } from "@eslint/compat";
import typescriptEslint from "@typescript-eslint/eslint-plugin";
import typescriptEslintParser from "@typescript-eslint/parser";
import noImport from "eslint-plugin-import";
import jsxA11y from "eslint-plugin-jsx-a11y";
import prettier from "eslint-plugin-prettier";
import eslintConfigPrettier from "eslint-config-prettier";
import react from "eslint-plugin-react";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import storybook from "eslint-plugin-storybook";
import globals from "globals";
import path from "path";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url); // get the resolved path to the file
const __dirname = path.dirname(__filename); // get the name of the directory

export default [
  // global ignores
  {
    ignores: ["dist/**", "scripts/**"],
  },
  eslint.configs.recommended,
  {
    files: ["**/*.ts", "**/*.tsx"],
    languageOptions: {
      ecmaVersion: "latest",
      globals: {
        ...globals.browser,
      },
      parser: typescriptEslintParser,
      parserOptions: {
        ecmaVersion: "latest",
        sourceType: "module",
        tsconfigRootDir: __dirname,
      },
    },
    plugins: {
      import: noImport,
      "jsx-a11y": jsxA11y,
      react,
      "react-hooks": fixupPluginRules(reactHooks),
      "react-refresh": reactRefresh,
      storybook,
      "@typescript-eslint": typescriptEslint,
      prettier,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "no-unused-vars": "off",
      "@typescript-eslint/no-unused-vars": [
        "error",
        {
          argsIgnorePattern: "^_",
          varsIgnorePattern: "^_",
          caughtErrorsIgnorePattern: "^_",
        },
      ],
      "react-refresh/only-export-components": [
        "warn",
        { allowConstantExport: true },
      ],
      quotes: ["error", "double"],
      "no-console": ["error", { allow: ["warn", "error"] }],
      "import/no-unresolved": "off",
      "sort-imports": [
        "error",
        {
          ignoreCase: true,
          ignoreDeclarationSort: true,
        },
      ],
      "import/order": [
        "error",
        {
          groups: [
            "external",
            "builtin",
            "internal",
            "parent",
            "sibling",
            "index",
          ],
          pathGroups: [
            {
              pattern: "assets",
              group: "internal",
            },
            {
              pattern: "common",
              group: "internal",
            },
            {
              pattern: "components",
              group: "internal",
            },
            {
              pattern: "motion/react",
              group: "external",
              position: "before",
            },
            {
              pattern: "pages",
              group: "internal",
            },
            {
              pattern: "react",
              group: "external",
              position: "before",
            },
            {
              pattern: "react-router-dom",
              group: "external",
              position: "before",
            },
            {
              pattern: "react-dom/client",
              group: "external",
              position: "before",
            },
            {
              pattern: "recharts",
              group: "external",
              position: "before",
            },
            {
              pattern: "tailwindcss/resolveConfig",
              group: "external",
              position: "before",
            },
            {
              pattern: "clsx",
              group: "external",
              position: "before",
            },
            {
              pattern: "@testing-library/react",
              group: "external",
              position: "before",
            },
            {
              pattern: "vitest",
              group: "external",
              position: "before",
            },
          ],
          pathGroupsExcludedImportTypes: ["internal"],
          alphabetize: {
            order: "asc",
            caseInsensitive: true,
          },
        },
      ],
      "prettier/prettier": "error",
    },
  },
  eslintConfigPrettier,
];
