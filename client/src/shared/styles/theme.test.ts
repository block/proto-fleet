// @vitest-environment node
import { describe, expect, test } from "vitest";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const themeCssPath = resolve(dirname(fileURLToPath(import.meta.url)), "theme.css");
const themeCss = readFileSync(themeCssPath, "utf8");

const escapeRegex = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

const getBlock = (selector: string) => {
  const selectorMatch = new RegExp(`${escapeRegex(selector)}\\s*\\{`, "m").exec(themeCss);

  if (!selectorMatch || selectorMatch.index === undefined) {
    throw new Error(`Missing CSS block for selector: ${selector}`);
  }

  const openBraceIndex = themeCss.indexOf("{", selectorMatch.index);

  if (openBraceIndex === -1) {
    throw new Error(`Missing CSS block for selector: ${selector}`);
  }

  let depth = 1;

  for (let index = openBraceIndex + 1; index < themeCss.length; index += 1) {
    const char = themeCss[index];

    if (char === "{") {
      depth += 1;
    } else if (char === "}") {
      depth -= 1;

      if (depth === 0) {
        return themeCss.slice(openBraceIndex + 1, index);
      }
    }
  }

  throw new Error(`Missing CSS block for selector: ${selector}`);
};

const getTokenValue = (block: string, token: string) => {
  const matches = block.matchAll(new RegExp(`${escapeRegex(token)}:\\s*([^;]+);`, "g"));
  let value: string | null = null;

  for (const match of matches) {
    value = match[1].trim();
  }

  if (value === null) {
    throw new Error(`Missing CSS token: ${token}`);
  }

  return value;
};

describe("theme tokens", () => {
  test("overrides the surface overlay token for dark mode", () => {
    const rootBlock = getBlock("@theme");
    const darkBlock = getBlock('[data-theme="dark"]');

    expect(getTokenValue(rootBlock, "--color-surface-overlay")).toBe("rgb(var(--base_black_100) / 5%)");
    expect(getTokenValue(darkBlock, "--color-surface-overlay")).toBe("rgb(var(--base_white_100) / 5%)");
  });
});
