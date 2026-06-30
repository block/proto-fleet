import { describe, expect, it } from "vitest";

import { type CatalogEntry, missingDependencies, withRequiredReads } from "./permissionCatalog";

// Minimal catalog covering the keys the dependency rules reference.
const catalog: CatalogEntry[] = [
  { key: "fleet:read", description: "View the dashboard, miner list, and live telemetry.", resource: "fleet" },
  { key: "miner:read", description: "View a miner's details, status, and error history.", resource: "miner" },
  { key: "miner:reboot", description: "Reboot a miner.", resource: "miner" },
  { key: "miner:stop_mining", description: "Stop mining on a miner.", resource: "miner" },
  { key: "miner:set_power_target", description: "Change a miner's power target.", resource: "miner" },
  { key: "miner:firmware_update", description: "Install firmware updates on a miner.", resource: "miner" },
  { key: "rack:read", description: "List racks at a site.", resource: "rack" },
  { key: "schedule:read", description: "View scheduled miner actions.", resource: "schedule" },
  { key: "schedule:manage", description: "Create, edit, pause, resume, and delete schedules.", resource: "schedule" },
];

describe("missingDependencies", () => {
  it("flags the reads and at least one action schedule:manage needs", () => {
    expect(missingDependencies(["schedule:manage"], catalog).sort()).toEqual(
      ["fleet:read", "miner:read", "miner:reboot", "miner:set_power_target", "miner:stop_mining", "rack:read"].sort(),
    );
  });

  it("stops suggesting the other actions once one is held (oneOf)", () => {
    const selected = withRequiredReads(["schedule:manage", "miner:reboot"], catalog);
    // miner:reboot satisfies the "at least one action" set and pulls in
    // miner:read + fleet:read via required reads, leaving only rack:read.
    expect(missingDependencies(selected, catalog)).toEqual(["rack:read"]);
  });

  it("flags reboot access for firmware updates", () => {
    expect(missingDependencies(["miner:firmware_update"], catalog)).toEqual(["miner:reboot"]);
  });

  it("returns nothing once dependencies are satisfied", () => {
    const selected = ["schedule:manage", "fleet:read", "miner:read", "rack:read", "miner:reboot"];
    expect(missingDependencies(selected, catalog)).toEqual([]);
  });

  it("skips dependencies the catalog does not publish", () => {
    const trimmed = catalog.filter((entry) => entry.key !== "miner:set_power_target");
    expect(missingDependencies(["schedule:manage"], trimmed)).not.toContain("miner:set_power_target");
  });

  it("reports nothing for selections without functional dependencies", () => {
    expect(missingDependencies(["miner:reboot", "miner:read", "fleet:read"], catalog)).toEqual([]);
  });
});
