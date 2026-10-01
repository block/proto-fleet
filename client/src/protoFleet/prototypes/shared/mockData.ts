/**
 * Mock snapshot builders for the Lab. Used to stand up the shared view before
 * any strategy is wired to real data, and as a deterministic fallback.
 */
import type { AsicCell, AsicHealth, HashboardSummary, SingleMinerSnapshot } from "./types";

function buildAsics(count: number, baseTemp: number, seed: number): AsicCell[] {
  return Array.from({ length: count }, (_, index) => {
    // Deterministic pseudo-variation so the grid looks alive without randomness
    // (Math.random is banned in some contexts and hurts reproducibility).
    const wobble = ((index * 7 + seed * 13) % 11) - 5;
    const tempC = baseTemp + wobble;
    let health: AsicHealth = "ok";
    if (tempC >= baseTemp + 4) health = "warn";
    if (tempC >= baseTemp + 8) health = "error";
    if ((index * 3 + seed) % 37 === 0) health = "off";
    return {
      index,
      tempC: health === "off" ? null : tempC,
      hashrateThs: health === "off" ? 0 : 1.1 + wobble / 50,
      health,
    };
  });
}

function buildHashboards(boards: number, asicsPerBoard: number): HashboardSummary[] {
  return Array.from({ length: boards }, (_, index) => {
    const asics = buildAsics(asicsPerBoard, 62 + index * 2, index + 1);
    const live = asics.filter((a) => a.health !== "off");
    const hashrateThs = live.reduce((sum, a) => sum + (a.hashrateThs ?? 0), 0);
    const tempC = live.length ? Math.max(...live.map((a) => a.tempC ?? 0)) : null;
    return {
      serialNumber: `HB-${1000 + index}`,
      index,
      tempC,
      hashrateThs,
      asics,
    };
  });
}

export interface MockSnapshotOptions {
  name?: string;
  mdkVersion?: string;
  source: string;
  dataPath: SingleMinerSnapshot["dataPath"];
  boards?: number;
  asicsPerBoard?: number;
  ipAddress?: string;
}

export function buildMockSnapshot(opts: MockSnapshotOptions): SingleMinerSnapshot {
  const hashboards = buildHashboards(opts.boards ?? 3, opts.asicsPerBoard ?? 66);
  const hashrateThs = hashboards.reduce((sum, hb) => sum + (hb.hashrateThs ?? 0), 0);
  const tempC = Math.max(...hashboards.map((hb) => hb.tempC ?? 0));
  return {
    identity: {
      name: opts.name ?? "proto-sim-01",
      model: "Proto Alpha",
      firmware: "1.4.2",
      mdkVersion: opts.mdkVersion ?? "MDK v1",
      macAddress: "02:42:0a:ff:00:01",
      serialNumber: "PROTO-SIM-0001",
      ipAddress: opts.ipAddress,
    },
    status: "mining",
    kpis: {
      hashrateThs,
      tempC,
      powerW: 3200 + Math.round(hashrateThs * 4),
    },
    hashboards,
    dataPath: opts.dataPath,
    source: opts.source,
    updatedAt: undefined,
  };
}
