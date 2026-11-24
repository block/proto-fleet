// @vitest-environment node
// Uses the ESLint programmatic API plus Node's fs/path, which the default jsdom
// environment polyfills with browser shims — force Node for this file.
import { beforeAll, describe, expect, it } from "vitest";
import { ESLint } from "eslint";
import { existsSync } from "fs";
import { dirname, resolve } from "path";
import { fileURLToPath } from "url";

// Guards the `no-restricted-imports` boundary in eslint.config.js that keeps a
// generated MDK client private to its adapter. The rule is the ONLY thing
// stopping code outside an adapter from deep-importing its generated/ folder
// (bypassing the public barrel), so a glob refactor that silently stops
// matching would reopen the leak with nothing else to catch it. These tests
// lint synthetic snippets through the real config and assert the rule fires on
// a forbidden import and stays quiet on the allowed ones.

// Walk up from this file to locate the client dir that owns eslint.config.js,
// so the test works regardless of the vitest `root` / cwd setting.
const findClientRoot = () => {
  let dir = dirname(fileURLToPath(import.meta.url));
  for (let i = 0; i < 8; i++) {
    if (existsSync(resolve(dir, "eslint.config.js"))) return dir;
    const parent = dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  throw new Error("Could not locate eslint.config.js above the test file");
};

const BOUNDARY_MESSAGE = "Generated MDK clients are private to their adapter";

let eslint: ESLint;

const boundaryViolations = async (code: string, filePath: string) => {
  const [result] = await eslint.lintText(code, { filePath });
  return result.messages.filter((m) => m.ruleId === "no-restricted-imports" && m.message.includes(BOUNDARY_MESSAGE));
};

describe("generated-client adapter boundary (eslint no-restricted-imports)", () => {
  beforeAll(() => {
    const clientRoot = findClientRoot();
    eslint = new ESLint({
      cwd: clientRoot,
      overrideConfigFile: resolve(clientRoot, "eslint.config.js"),
    });
  });

  it("rejects an outside-adapter deep import of a generated client", async () => {
    const violations = await boundaryViolations(
      `import { Api } from "@/protoOS/api/adapters/mdkV1/generated/generatedApi";\n`,
      "src/protoOS/api/hooks/__boundaryProbe__.ts",
    );
    expect(violations).toHaveLength(1);
  });

  it("rejects an outside-adapter import of the generated/ folder itself", async () => {
    const violations = await boundaryViolations(
      `import { Api } from "@/protoOS/api/adapters/mdkV1/generated";\n`,
      "src/protoOS/api/hooks/__boundaryProbe__.ts",
    );
    expect(violations).toHaveLength(1);
  });

  it("allows importing through the adapter's public barrel", async () => {
    const violations = await boundaryViolations(
      `import { Api } from "@/protoOS/api/adapters/mdkV1";\n`,
      "src/protoOS/api/hooks/__boundaryProbe__.ts",
    );
    expect(violations).toHaveLength(0);
  });

  it("allows the adapter to reach its own generated client via a relative path", async () => {
    const violations = await boundaryViolations(
      `import { Api } from "./generated/generatedApi";\n`,
      "src/protoOS/api/adapters/mdkV1/MdkV1Adapter.ts",
    );
    expect(violations).toHaveLength(0);
  });
});
