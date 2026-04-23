import eslint from "@eslint/js";
import { fixupPluginRules } from "@eslint/compat";
import typescriptEslint from "@typescript-eslint/eslint-plugin";
import typescriptEslintParser from "@typescript-eslint/parser";
import importX from "eslint-plugin-import-x";
import jsxA11y from "eslint-plugin-jsx-a11y";
import playwright from "eslint-plugin-playwright";
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
    ignores: ["**/dist/**", "scripts/**", "**/playwright-report/**", "**/test-results/**", "**/api/generated/**"],
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
    linterOptions: {
      // Pre-existing `eslint-disable ... react-hooks/refs` and
      // `react-hooks/immutability` comments are left in place while those
      // rules remain off (see below). They become unused-directives in that
      // state, so silence the check until those rules are turned back on via
      // their follow-up adoption issues.
      reportUnusedDisableDirectives: "off",
    },
    plugins: {
      "import-x": importX,
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
      // React Compiler rules added to the `recommended` preset in
      // eslint-plugin-react-hooks 7.1. The rules listed below remain
      // disabled and are being adopted incrementally via follow-up issues.
      "react-hooks/refs": "off",
      "react-hooks/immutability": "off",
      "no-unused-vars": "off",
      "@typescript-eslint/no-unused-vars": [
        "error",
        {
          argsIgnorePattern: "^_",
          varsIgnorePattern: "^_",
          caughtErrorsIgnorePattern: "^_",
        },
      ],
      "react-refresh/only-export-components": ["warn", { allowConstantExport: true }],
      quotes: ["error", "double"],
      "no-console": ["error", { allow: ["warn", "error"] }],
      "import-x/no-unresolved": "off",
      "sort-imports": [
        "error",
        {
          ignoreCase: true,
          ignoreDeclarationSort: true,
        },
      ],
      "import-x/order": [
        "error",
        {
          groups: ["external", "builtin", "internal", "parent", "sibling", "index"],
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
  {
    files: ["e2eTests/**/*.ts"],
    languageOptions: {
      ecmaVersion: "latest",
      globals: {
        ...globals.node,
      },
      parser: typescriptEslintParser,
      parserOptions: {
        ecmaVersion: "latest",
        sourceType: "module",
      },
    },
    plugins: {
      "@typescript-eslint": typescriptEslint,
      playwright,
      prettier,
    },
    rules: {
      ...typescriptEslint.configs.recommended.rules,
      ...playwright.configs["flat/recommended"].rules,
      quotes: ["error", "double"],
      semi: ["error", "always"],
      "@typescript-eslint/no-unused-vars": ["warn", { argsIgnorePattern: "^_" }],
      "prettier/prettier": "error",
    },
  },
  eslintConfigPrettier,
];
