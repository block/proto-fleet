import { describe, expect, it } from "vitest";

import { type CatalogEntry, dependencyGaps, withRequiredReads } from "./permissionCatalog";

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

describe("dependencyGaps", () => {
  it("gives schedule:manage no hard requirements, only a choose-one-of action set", () => {
    const gaps = dependencyGaps(["schedule:manage"], catalog);
    // No forced reads: the chosen action pulls its own read floor via
    // withRequiredReads, and rack/miner reads are only for optional targeting.
    expect(gaps.required).toEqual([]);
    // The miner actions are a "choose at least one" set, never auto-added.
    expect(gaps.chooseOneOf).toEqual([["miner:reboot", "miner:stop_mining", "miner:set_power_target"]]);
  });

  it("never lists oneOf members as hard-required", () => {
    const gaps = dependencyGaps(["schedule:manage"], catalog);
    expect(gaps.required).not.toContain("miner:reboot");
    expect(gaps.required).not.toContain("miner:stop_mining");
    expect(gaps.required).not.toContain("miner:set_power_target");
  });

  it("stops suggesting the action set once one member is held (oneOf)", () => {
    const selected = withRequiredReads(["schedule:manage", "miner:reboot"], catalog);
    // miner:reboot satisfies the "at least one action" set and pulls in its own
    // read floor, so nothing is left to flag.
    const gaps = dependencyGaps(selected, catalog);
    expect(gaps.required).toEqual([]);
    expect(gaps.chooseOneOf).toEqual([]);
  });

  it("treats reboot access for firmware updates as a hard requirement", () => {
    const gaps = dependencyGaps(["miner:firmware_update"], catalog);
    expect(gaps.required).toEqual(["miner:reboot"]);
    expect(gaps.chooseOneOf).toEqual([]);
  });

  it("returns no gaps once dependencies are satisfied", () => {
    const selected = ["schedule:manage", "fleet:read", "miner:read", "rack:read", "miner:reboot"];
    const gaps = dependencyGaps(selected, catalog);
    expect(gaps.required).toEqual([]);
    expect(gaps.chooseOneOf).toEqual([]);
  });

  it("skips choose-one-of members the catalog does not publish", () => {
    const trimmed = catalog.filter((entry) => entry.key !== "miner:set_power_target");
    const gaps = dependencyGaps(["schedule:manage"], trimmed);
    expect(gaps.chooseOneOf).toEqual([["miner:reboot", "miner:stop_mining"]]);
  });

  it("omits a choose-one-of set entirely when the catalog publishes none of its members", () => {
    const trimmed = catalog.filter((entry) => entry.resource !== "miner" || entry.key === "miner:read");
    const gaps = dependencyGaps(["schedule:manage"], trimmed);
    expect(gaps.chooseOneOf).toEqual([]);
  });

  it("reports no gaps for selections without functional dependencies", () => {
    const gaps = dependencyGaps(["miner:reboot", "miner:read", "fleet:read"], catalog);
    expect(gaps.required).toEqual([]);
    expect(gaps.chooseOneOf).toEqual([]);
  });
});
