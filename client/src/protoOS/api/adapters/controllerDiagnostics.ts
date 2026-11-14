/** Freshness the reporting controller attached to an observation. Age is judged separately. */
export type ReportedFreshness = "fresh" | "stale" | "unspecified";

/** One independently aged HA observation, kept in source time rather than receipt time. */
export interface ControllerObservation {
  freshness: ReportedFreshness;
  observedAtMs?: number;
  lastSuccessfulAtMs?: number;
  sourceId?: string;
  detail?: string;
}

export type ControllerRole = "primary" | "standby" | "fenced" | "unknown";
export type ControllerRecoveryState = "steady" | "reconciling" | "degraded" | "unknown";
export type ControllerRecoveryPhase =
  "steady" | "database" | "controlState" | "assignments" | "flotillaSync" | "unknown";

export interface ControllerRecovery {
  state: ControllerRecoveryState;
  phase: ControllerRecoveryPhase;
  expectedBoards?: number;
  reconciledBoards?: number;
  updatedAtMs?: number;
  detail?: string;
}

export interface ControllerDiagnostics {
  /** Configured server ID. Absent when no report names the controller. */
  id?: string;
  /** True for the controller that answered this request. */
  local: boolean;
  /** False when the controller is expected but no member report describes it. */
  reported: boolean;
  hostname?: string;
  /** Software version. Only the answering controller reports its own. */
  version?: string;
  role: ControllerRole;
  /** Last reported container write authority, qualified by `roleObservation`. */
  writePrimary?: boolean;
  roleObservation?: ControllerObservation;
  reachable?: boolean;
  reachabilityObservation?: ControllerObservation;
  recovery?: ControllerRecovery;
}

export type HaIdentityState = "confirmed" | "conflicted" | "unavailable" | "unknown";

export type HaReport =
  | {
      status: "reported";
      enabled: boolean;
      clusterId?: string;
      identityState: HaIdentityState;
      /** False never means that no prior identity exists. */
      inventoryReady: boolean;
      observedAtMs?: number;
      conflicts: string[];
      detail?: string;
    }
  /** The controller does not serve HA status (single controller or older software). */
  | { status: "unsupported" }
  | { status: "unavailable"; message: string };

export type ControllerStatusReport =
  { status: "reported"; databaseConnected: boolean; dcsHealthy?: boolean } | { status: "unavailable"; message: string };

export interface ControllerDiagnosticsResult {
  ha: HaReport;
  status: ControllerStatusReport;
  /** The answering controller first, then its configured peer. */
  controllers: ControllerDiagnostics[];
}
