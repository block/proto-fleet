/**
 * Shared mapping from the prototype's `MinerStatus` onto the design-system
 * StatusCircle status + a human label. Used by both the miner view header and
 * the mini miners list so status reads consistently everywhere.
 */
import type { MinerStatus } from "./types";
import { statuses } from "@/shared/components/StatusCircle";
import type { StatusCircleStatus } from "@/shared/components/StatusCircle/constants";

export const STATUS_META: Record<MinerStatus, { label: string; circle: StatusCircleStatus }> = {
  mining: { label: "Mining", circle: statuses.normal },
  paused: { label: "Paused", circle: statuses.warning },
  offline: { label: "Offline", circle: statuses.inactive },
  error: { label: "Error", circle: statuses.error },
};
