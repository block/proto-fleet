import type { MiningGroupPoolSettings, PoolBoardAssignment } from "./poolConfiguration";
import type { AdapterRequestOptions } from "./types";

export interface PoolAssignmentTarget {
  macs: string[];
  /** Exact board sets captured when the operator selects whole modules. */
  modules?: string[][];
}
export interface AssignmentPool {
  id?: string;
  preservePassword?: boolean;
  enabled?: boolean;
  shareWeight?: number;
  name: string;
  url: string;
  username: string;
  password: string;
}
export interface SavedPoolSet {
  id: string;
  name: string;
  revision: string;
  mining: boolean;
  pools: MiningGroupPoolSettings[];
}
export interface LocalPoolConfiguration extends SavedPoolSet {
  assignedCount: number;
  deletable: boolean;
  isDefault: boolean;
}

export class PoolDeletionUnconfirmed extends Error {
  constructor(readonly cause?: unknown) {
    super("Deletion is not confirmed. Close this dialog to refresh pools.");
  }
}
export interface PoolAssignmentInspection {
  boards: PoolBoardAssignment[];
  savedPools: SavedPoolSet[];
  currentPoolSetId?: string;
  maxPools: number;
  requiresActivation: boolean;
}
export interface PoolAssignmentPlan {
  configurationOnly?: boolean;
  editingRevision?: string;
  originalPools?: MiningGroupPoolSettings[];
  id: string;
  name: string;
  target: PoolAssignmentTarget;
  boards: PoolBoardAssignment[];
  stamp: string;
  pools: AssignmentPool[];
  savedPoolSet?: SavedPoolSet;
  mining: boolean;
  requiresActivation: boolean;
  destinationId?: string;
  destinationRef: string;
  transfers: Array<{ source: string; macs: string[] }>;
}
/** Nonsecret recovery record. Passwords and write payloads must never be persisted. */
export interface PoolAssignmentAttempt {
  name?: string;
  configurationOnly?: boolean;
  id: string;
  macs: string[];
  destinationId?: string;
  destinationRevision?: string;
  unconfirmedTransferRevision?: string;
  destinationRef: string;
  pools: MiningGroupPoolSettings[];
  configurationConfirmed: boolean;
  operationIds: string[];
  phase: string;
  /** Legacy mining was running before activation and still needs a safe, idempotent restore. */
  restoreLegacyMining?: boolean;
}
export interface PoolAssignmentStatus {
  state: "pending" | "saved" | "partial" | "unknown" | "failed";
  message: string;
  confirmed: number;
  total: number;
  blockers: Array<{ mac?: string; reason: string }>;
  remainingMacs?: string[];
}
export interface PoolAssignmentChoice {
  editPoolSetId?: string;
  expectedRevision?: string;
  expectedPools?: MiningGroupPoolSettings[];
  createOnly?: boolean;
  name?: string;
  pools: AssignmentPool[];
  savedPoolSetId?: string;
}
export interface PoolAssignmentService {
  listConfigurations(params?: AdapterRequestOptions): Promise<LocalPoolConfiguration[]>;
  listConfigurationPage(
    offset: number,
    limit: number,
    params?: AdapterRequestOptions,
  ): Promise<{ configurations: LocalPoolConfiguration[]; total: number }>;
  deleteConfiguration(id: string, revision: string, params?: AdapterRequestOptions): Promise<void>;
  inspect(target: PoolAssignmentTarget, params?: AdapterRequestOptions): Promise<PoolAssignmentInspection>;
  review(
    target: PoolAssignmentTarget,
    choice: PoolAssignmentChoice,
    params?: AdapterRequestOptions,
  ): Promise<PoolAssignmentPlan>;
  apply(
    plan: PoolAssignmentPlan,
    track: (attempt: PoolAssignmentAttempt) => void,
    params?: AdapterRequestOptions,
  ): Promise<PoolAssignmentStatus>;
  status(attempt: PoolAssignmentAttempt, params?: AdapterRequestOptions): Promise<PoolAssignmentStatus>;
  restoreMining(
    attempt: PoolAssignmentAttempt,
    track: (attempt: PoolAssignmentAttempt) => void,
    params?: AdapterRequestOptions,
  ): Promise<PoolAssignmentStatus>;
}
