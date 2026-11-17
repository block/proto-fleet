import type { AdapterRequestOptions } from "./types";

export interface MappingBoard {
  mac: string;
  moduleId?: string;
  tank?: number;
  module?: number;
  position?: number;
  expectedLocation?: string;
  expectedFeed?: string;
  assignedFeed?: string;
  observedFeed?: string;
  switchPort?: string;
  status: string;
  reasons: string[];
  coverage: string[];
  verifiedAt?: string;
  unlocated: boolean;
  conflict: boolean;
}

export interface MappingAssessment {
  boards: MappingBoard[];
  mode: string;
  /** The report can advertise wiring verification without a hard mapping manager. */
  hardMappingAvailable: boolean;
  /** A partial read still reports unlocated boards, but cannot authorize a write. */
  unavailable?: string;
  configurationReasons: string[];
  deferredReason?: string;
  changes: { at?: string; mac: string; detail: string }[];
  historyIncomplete: boolean;
}

export interface MappingRun {
  id: string;
  status: "planned" | "running" | "completed" | "failed" | "canceled" | "unknown";
  phase: string;
  currentFeed?: string;
  feeds: { id: string; boards: number; complete: boolean; timedOut: boolean }[];
  error?: string;
  warnings: string[];
}

export interface FeedMapping {
  assess(params?: AdapterRequestOptions): Promise<MappingAssessment>;
  active(params?: AdapterRequestOptions): Promise<MappingRun | null>;
  get(id: string, params?: AdapterRequestOptions): Promise<MappingRun>;
  plan(params?: AdapterRequestOptions): Promise<MappingRun>;
  start(feedIds: string[], params?: AdapterRequestOptions): Promise<MappingRun>;
  cancel(id: string, params?: AdapterRequestOptions): Promise<MappingRun>;
  verify(macs: string[], params?: AdapterRequestOptions): Promise<void>;
}

export const mappingRunActive = (run?: MappingRun | null) =>
  !!run && (run.status === "planned" || run.status === "running" || run.status === "unknown");

export const mappingError = (error: unknown): string => {
  const value = error as { error?: { message?: string }; message?: string } | undefined;
  return value?.error?.message || value?.message || "The controller did not respond.";
};
