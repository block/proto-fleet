/** CT1 (Frigate) hashboard event history and recovery evidence, read by ProtoOS and Fleet. */

import type { ControllerRecordsRead } from "@/shared/features/controllerLogs/types";

/** Frigate returns at most seven days of events per read. */
export const BOARD_EVENT_WINDOW_MAX_MS = 7 * 24 * 60 * 60 * 1000;
/** Frigate keeps recovery evidence for 30 days, at most ten snapshots per board. */
export const RECOVERY_EVIDENCE_RETENTION_DAYS = 30;
export const RECOVERY_EVIDENCE_PER_BOARD = 10;

/**
 * Past events only. The controller also reports a board's current isolation in
 * this history; adapters drop it, because it is current state, not history.
 */
export type BoardEventKind = "stateChange" | "error" | "warning" | "lastShutdown" | "stackDropout";

export interface BoardEvent {
  /** Stable within one read. */
  id: string;
  kind: BoardEventKind;
  /** ISO 8601. For aggregated causes, the first health sample in the window that reported it. */
  time: string;
  /** Last health sample in the window that reported the cause. */
  lastSeen?: string;
  /** Health samples in the window that reported the cause. */
  samples?: number;
  /** Wire cause code, such as HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE. */
  cause?: string;
  /** Wire mining states, such as MINING_STATE_MINING, for state changes. */
  fromState?: string;
  toState?: string;
  /** Raw zero-based firmware stack index for stack dropouts. */
  stack?: number;
}

export interface BoardEventQuery {
  /** ISO 8601, inclusive. */
  start: string;
  /** ISO 8601, exclusive. */
  end: string;
}

export interface BoardEventPage {
  /** Oldest first. */
  events: BoardEvent[];
  /** The [start, end) window the controller actually read, after its default and clamp. */
  window?: BoardEventQuery;
  /** When the page was read, ISO 8601. */
  fetchedAt: string;
}

export interface RecoveryEvidenceSummary {
  id: string;
  /** Links snapshots of the same recovery episode or power cycle. */
  episodeId?: string;
  capturedAt?: string;
  /** Server ID of the controller that took the snapshot. */
  capturedBy?: string;
  /** Wire trigger: power_recovery, power_recovery_capture_only or ready_recovery. */
  trigger?: string;
  /** Wire cause codes that made the board eligible for recovery. */
  causes: string[];
  /** The controller dropped data to keep the snapshot within its size cap. */
  truncated: boolean;
}

export interface RecoveryEvidenceRecord {
  summary: RecoveryEvidenceSummary;
  /** The stored snapshot document, as the controller returned it. */
  json: string;
}

/** How an app reads one board's history. Transport and board identity stay with the app. */
export interface BoardHistorySource {
  readEvents?: ControllerRecordsRead<BoardEventQuery, BoardEventPage>;
  /** Newest first. */
  listEvidence?: ControllerRecordsRead<void, RecoveryEvidenceSummary[]>;
  readEvidence?: ControllerRecordsRead<string, RecoveryEvidenceRecord>;
}
