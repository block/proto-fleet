/** Controller-reported configuration. These records do not prove application on a board. */
export interface PoolBoardAssignment {
  mac: string;
  location?: string;
}

export interface ContainerPoolConfiguration {
  mode: "container" | "groups";
  suspended: boolean;
  groups: Array<{ id: string; name: string; revision: string; boards: PoolBoardAssignment[] }>;
  unassignedBoards: PoolBoardAssignment[];
  locationsAvailable: boolean;
  pendingOperations: Array<{ id: string; kind: string; phase: string; error?: string; boards: string[] }>;
  inventoryIssues: Array<{ groupId?: string; mac?: string; reason: string }>;
}

/** Terminal receipts can appear if an operation ends between the controller's list and detail reads. */
export const hasPendingPoolOperations = (operations: ContainerPoolConfiguration["pendingOperations"]) =>
  operations.some(({ phase }) => !["COMPLETE", "FAILED"].includes(phase.trim().toUpperCase()));

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
