import { macKey } from "@/protoOS/utils/macAddress";

/** Controller-reported configuration. These records do not prove application on a board. */
export interface PoolBoardAssignment {
  mac: string;
  location?: string;
  isolated?: boolean;
}

export interface ContainerPoolConfiguration {
  mode: "container" | "groups" | "unsupported";
  suspended: boolean;
  groups: Array<{ id: string; name: string; revision: string; boards: PoolBoardAssignment[] }>;
  unassignedBoards: PoolBoardAssignment[];
  locationsAvailable: boolean;
  assignmentInventoryError?: string;
  pendingOperations: Array<{ id: string; kind: string; phase: string; error?: string; boards: string[] }>;
  inventoryIssues: Array<{ groupId?: string; mac?: string; reason: string }>;
}

/** Terminal receipts can appear if an operation ends between the controller's list and detail reads. */
export const hasPendingPoolOperations = (operations: ContainerPoolConfiguration["pendingOperations"]) =>
  operations.some(({ phase }) => !["COMPLETE", "FAILED"].includes(phase.trim().toUpperCase()));

const unassignedPoolAssignmentBlockerMessages = new Map([
  ["missing_verified_feed", "Verify this hashboard's power feed before assigning it."],
  ["missing_module_identity", "Verify this hashboard's module identity before assigning it."],
  ["conflicting_psu_identity", "Resolve this hashboard's power supply identity conflict before assigning it."],
  ["missing_current_binding", "Restore this hashboard's controller connection before assigning it."],
]);

export const POOL_ASSIGNMENT_ISOLATION_BLOCKER = "Remove isolation before assigning this hashboard.";

export function getUnassignedPoolAssignmentBlockers(
  issues: ContainerPoolConfiguration["inventoryIssues"],
  isolatedMacs: Iterable<string>,
) {
  const blockers = new Map<string, string>();
  for (const mac of isolatedMacs) blockers.set(macKey(mac), POOL_ASSIGNMENT_ISOLATION_BLOCKER);
  for (const { mac, reason } of issues) {
    const key = mac ? macKey(mac) : undefined;
    const blocker = unassignedPoolAssignmentBlockerMessages.get(reason);
    if (key && blocker && !blockers.has(key)) blockers.set(key, blocker);
  }
  return blockers;
}

export interface MiningGroupPoolSettings {
  id: string;
  name: string;
  url: string;
  /** The complete pool username. Frigate has no separate worker-name field. */
  username: string;
  priority: number;
  enabled: boolean;
  shareWeight?: number;
}
