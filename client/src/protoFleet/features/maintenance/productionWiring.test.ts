import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import process from "node:process";

const root = join(process.cwd(), "src/protoFleet/features/maintenance");
const files = (directory: string): string[] =>
  readdirSync(directory).flatMap((name) => {
    const path = join(directory, name);
    if (statSync(path).isDirectory()) return files(path);
    return /\.(ts|tsx)$/.test(name) && !/\.(test|stories)\./.test(name) && name !== "productionWiring.test.ts"
      ? [path]
      : [];
  });
describe("maintenance production wiring", () => {
  it("contains no prototype fixtures or placeholder adapters", () => {
    const source = files(root)
      .map((path) => readFileSync(path, "utf8"))
      .join("\n");
    expect(source).not.toMatch(/from ["'].*mockData["']/);
    expect(source).not.toMatch(/wire to (maintenanceClient|inventoryClient|API)/);
    expect(source).not.toContain("Export CSV");
  });
});
