import type { FirmwareTransitionState } from "@/protoFleet/features/rollout/rolloutTypes";
import { statuses } from "@/shared/components/StatusCircle";

interface FirmwareTransitionDisplay {
  tableLabel: string;
  countLabel: string;
  status: keyof typeof statuses;
}

export const firmwareTransitionDisplay: Record<FirmwareTransitionState, FirmwareTransitionDisplay> = {
  pending: {
    tableLabel: "Pending",
    countLabel: "pending",
    status: statuses.inactive,
  },
  updating: {
    tableLabel: "Updating firmware",
    countLabel: "updating firmware",
    status: statuses.error,
  },
  verifying: {
    tableLabel: "Verifying firmware",
    countLabel: "verifying firmware",
    status: statuses.warning,
  },
  confirmed: {
    tableLabel: "Confirmed",
    countLabel: "confirmed",
    status: statuses.normal,
  },
  needsAttention: {
    tableLabel: "Needs attention",
    countLabel: "needs attention",
    status: statuses.error,
  },
};
