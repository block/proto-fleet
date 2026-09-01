/**
 * The distilled single-miner contract shared by the prototype strategies.
 *
 * Deliberately minimal: identity + three KPIs + a hashboard/ASIC mini-grid +
 * one control. The ASIC grid is the intentional stressor — it's the one piece
 * the fleet server does not expose today, so every strategy has to prove how it
 * would source per-component data.
 *
 * Every strategy (fleet-native, adapter) maps its backend into this shape and
 * renders the same <SingleMinerView>. The proxy strategy deliberately does NOT
 * use this — its whole point is rendering per-version clients verbatim.
 */

export type MinerStatus = "mining" | "paused" | "offline" | "error";

/** Health of a single ASIC cell, drives the mini-grid color. */
export type AsicHealth = "ok" | "warn" | "error" | "off";

export interface AsicCell {
  /** 0-based position within the hashboard. */
  index: number;
  tempC: number | null;
  hashrateThs: number | null;
  health: AsicHealth;
}

export interface HashboardSummary {
  serialNumber: string;
  /** 0-based slot index on the control board. */
  index: number;
  tempC: number | null;
  hashrateThs: number | null;
  asics: AsicCell[];
}

export interface MinerIdentity {
  name: string;
  model: string;
  firmware: string;
  /** e.g. "MDK v1" / "MDK v2" — the axis the proxy/adapter strategies branch on. */
  mdkVersion: string;
  macAddress: string;
  serialNumber: string;
  /** Present when we reached the device directly (adapter / single-miner mode). */
  ipAddress?: string;
}

export interface MinerKpis {
  hashrateThs: number | null;
  tempC: number | null;
  powerW: number | null;
}

/** One hop in the "how did this data get here" ribbon. */
export interface DataPathStep {
  label: string;
  /** Short note, e.g. "Connect RPC", "REST /api/v1", "reverse proxy". */
  detail?: string;
}

export type MinerControlAction = "reboot" | "pause" | "resume";

export interface SingleMinerSnapshot {
  identity: MinerIdentity;
  status: MinerStatus;
  kpis: MinerKpis;
  hashboards: HashboardSummary[];
  /** Left→right chain rendered by the data-path ribbon. */
  dataPath: DataPathStep[];
  /**
   * Optional caveat rendered beneath the data-path ribbon — used to flag an
   * FPO (for-placement-only) step, e.g. a synthesized ASIC grid, and what a
   * production build would actually require. Mark the step it refers to with a
   * trailing "*".
   */
  dataPathNote?: string;
  /** Human label for where this snapshot came from, e.g. "Fleet server". */
  source: string;
  /** ISO timestamp of the reading, if known. */
  updatedAt?: string;
}

/** Handlers a strategy wires to the view's control button(s). */
export interface SingleMinerActions {
  onControl?: (action: MinerControlAction) => void | Promise<void>;
}
