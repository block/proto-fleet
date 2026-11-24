import type { MiningGroupPoolSettings, PoolBoardAssignment } from "./poolConfiguration";
import type { AdapterRequestOptions } from "./types";

export interface PoolAssignmentTarget {
  macs: string[];
  /** Require enabled mining groups and no existing membership for any selected board. */
  unassignedOnly?: boolean;
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
  defaultPoolRevision?: string;
  mining: boolean;
  pools: MiningGroupPoolSettings[];
}
export interface LocalPoolConfiguration extends SavedPoolSet {
  assignedCount: number;
  assignedMacs: string[];
  deletable: boolean;
  isDefault: boolean;
}

export interface PoolConfigurationUpdate {
  id: string;
  time?: string;
  action: string;
  result: string;
  macs: string[];
}

export interface PoolConfigurationUpdatePage {
  updates: PoolConfigurationUpdate[];
  limitReached: boolean;
}

export class PoolDeletionUnconfirmed extends Error {
  constructor(readonly cause?: unknown) {
    super("Deletion is not confirmed. Close this dialog to refresh pools.");
  }
}

export class WorkerNameUpdateRolledBack extends Error {
  constructor(readonly attemptId: string) {
    super("The controller refused to copy the pool configuration. Check the source pool settings before trying again.");
  }
}
export interface PoolAssignmentInspection {
  savedPools: Array<SavedPoolSet & { projectedBoards: number }>;
  currentPoolSetId?: string;
  mixedConfigurations: boolean;
  capacity: PoolAssignmentCapacity;
}
export interface PoolAssignmentCapacity {
  groupsUsed: number;
  maxGroups: number;
  maxBoards: number;
  maxPools: number;
}
export const MINING_GROUP_LIMIT_MESSAGE =
  "All mining groups are in use. Choose a saved configuration or delete an unused one.";
export interface LocalPoolConfigurationPage {
  configurations: LocalPoolConfiguration[];
  total: number;
  capacity: PoolAssignmentCapacity;
}
export interface PoolAssignmentPlan {
  configurationOnly?: boolean;
  workerNameOnly?: boolean;
  editingRevision?: string;
  editingDefaultPoolRevision?: string;
  originalPools?: MiningGroupPoolSettings[];
  id: string;
  name: string;
  target: PoolAssignmentTarget;
  boards: Array<PoolBoardAssignment & { physicalLocation?: string }>;
  stamp: string;
  pools: AssignmentPool[];
  savedPoolSet?: SavedPoolSet;
  usernameSource?: SavedPoolSet;
  capacity: PoolAssignmentCapacity & { destinationBoards: number };
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
  unassignedOnly?: boolean;
  destinationId?: string;
  destinationRevision?: string;
  destinationDefaultPoolRevision?: string;
  unconfirmedTransferRevision?: string;
  destinationRef: string;
  pools: MiningGroupPoolSettings[];
  configurationConfirmed: boolean;
  poolFingerprint?: string;
  operationIds: string[];
  phase: string;
  /** Legacy mining was running before activation. Check its state before closing recovery. */
  restoreLegacyMining?: boolean;
  /** Selected boards in other worker-name groups that have no submitted write. */
  unsubmittedWorkerNameBoards?: number;
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
  usernameSource?: SavedPoolSet;
}
export interface WorkerNameSource {
  configuration: SavedPoolSet;
  boards: WorkerNameBoard[];
}
export interface WorkerNameBoard extends PoolBoardAssignment {
  physicalLocation?: string;
  serialNumber?: string;
  model?: string;
  tank?: string;
  position?: string;
}
export interface WorkerNameChoice {
  namesByMac: Record<string, string>;
  sources: WorkerNameSource[];
}
export interface WorkerNameReview {
  groups: Array<{ assignment: PoolAssignmentPlan; workerName: string }>;
  unchangedCount: number;
  capacity: PoolAssignmentCapacity;
}

const invalidPoolUsernameCharacters = /[\s\p{C}]/u;
export const hasInvalidPoolUsernameCharacters = (value: string) => invalidPoolUsernameCharacters.test(value);

export const INVALID_WORKER_NAME_MESSAGE =
  "Use up to 100 characters without spaces or periods. For a worker name with periods, edit the complete pool username.";

export const isValidWorkerName = (name: string) =>
  Boolean(name) && !name.includes(".") && Array.from(name).length <= 100 && !hasInvalidPoolUsernameCharacters(name);

export function currentWorkerName(pools: MiningGroupPoolSettings[]) {
  return splitPoolUsername((pools.find((pool) => pool.enabled) ?? pools[0])?.username ?? "").worker;
}

function splitPoolUsername(username: string) {
  const trimmed = username.trim();
  const separator = trimmed.lastIndexOf(".");
  if (separator <= 0 || separator === trimmed.length - 1) return { base: trimmed, worker: "" };
  return { base: trimmed.slice(0, separator).trim(), worker: trimmed.slice(separator + 1).trim() };
}

// A direct controller reports complete usernames. These are suggestions for
// the review, which displays the complete values before committing them.
export function proposedWorkerNamePools(pools: MiningGroupPoolSettings[], workerName: string) {
  const primary = pools.find((pool) => pool.enabled) ?? pools[0];
  return pools.map((pool) => {
    const { base, worker } = splitPoolUsername(pool.username);
    const alreadyMatches = pool.username.trim().endsWith(`.${workerName}`);
    // A backup without a suffix can use a fixed username.
    const keepUsername = alreadyMatches || (!worker && pool !== primary);
    return { ...pool, username: keepUsername ? pool.username : base && `${base}.${workerName}` };
  });
}

export function changesWorkerNamePools(pools: MiningGroupPoolSettings[], workerName: string) {
  return proposedWorkerNamePools(pools, workerName).some(({ username }, index) => username !== pools[index].username);
}
export interface PoolAssignmentService {
  listConfigurationUpdates?(id: string, params?: AdapterRequestOptions): Promise<PoolConfigurationUpdatePage>;
  listConfigurations(params?: AdapterRequestOptions): Promise<LocalPoolConfiguration[]>;
  listConfigurationPage(
    offset: number,
    limit: number,
    params?: AdapterRequestOptions,
  ): Promise<LocalPoolConfigurationPage>;
  deleteConfiguration(id: string, revision: string, params?: AdapterRequestOptions): Promise<void>;
  inspect(target: PoolAssignmentTarget, params?: AdapterRequestOptions): Promise<PoolAssignmentInspection>;
  inspectWorkerNames(target: PoolAssignmentTarget, params?: AdapterRequestOptions): Promise<WorkerNameSource[]>;
  reviewWorkerNames(
    target: PoolAssignmentTarget,
    choice: WorkerNameChoice,
    params?: AdapterRequestOptions,
  ): Promise<WorkerNameReview>;
  applyWorkerNames(
    review: WorkerNameReview,
    callbacks: {
      track: (attempt: PoolAssignmentAttempt) => void;
      saved: (attempt: PoolAssignmentAttempt) => void;
    },
    params?: AdapterRequestOptions,
  ): Promise<PoolAssignmentStatus>;
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
  checkMiningRecovery(
    attempt: PoolAssignmentAttempt,
    controllerId: string,
    track: (attempt: PoolAssignmentAttempt) => void,
    params?: AdapterRequestOptions,
  ): Promise<PoolAssignmentStatus>;
}
