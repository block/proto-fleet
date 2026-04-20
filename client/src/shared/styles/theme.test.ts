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

const getGrayLevel = (value: string) => {
  const [grayLevel] = value.split(/\s+/).map(Number);

  return grayLevel;
};

describe("theme tokens", () => {
  test("defines the expected shared gray base tokens in the expected order", () => {
    const rootBlock = getBlock(":root");
    const baseGray2 = getTokenValue(rootBlock, "--base_gray_2");
    const baseGray5 = getTokenValue(rootBlock, "--base_gray_5");
    const baseGray50 = getTokenValue(rootBlock, "--base_gray_50");
    const baseGray60 = getTokenValue(rootBlock, "--base_gray_60");

    expect(baseGray2).toBe("250 250 250");
    expect(baseGray5).toBe("242 242 242");
    expect(getGrayLevel(baseGray2)).toBeGreaterThan(getGrayLevel(baseGray5));
    expect(getGrayLevel(baseGray50)).toBeGreaterThan(getGrayLevel(baseGray60));
  });

  test("overrides the surface overlay token for dark mode", () => {
    const rootBlock = getBlock("@theme");
    const darkBlock = getBlock('[data-theme="dark"]');

    expect(getTokenValue(rootBlock, "--color-surface-overlay")).toBe("rgb(var(--base_black_100) / 5%)");
    expect(getTokenValue(darkBlock, "--color-surface-overlay")).toBe("rgb(var(--base_white_100) / 5%)");
  });
});
