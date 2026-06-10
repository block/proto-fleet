import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { statuses } from "@/shared/components/StatusCircle/constants";

type StatusKey = keyof typeof statuses;

// Connection is inferred from the heartbeat: the agent calls UploadHeartbeat
// every 30s, so a last_seen within a couple intervals means "connected".
const CONNECTED_WINDOW_MS = 90 * 1000;

export const enrollmentStatusLabel = (status: FleetNodeEnrollmentStatus): string => {
  switch (status) {
    case FleetNodeEnrollmentStatus.PENDING:
      return "Pending";
    case FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION:
      return "Awaiting confirmation";
    case FleetNodeEnrollmentStatus.CONFIRMED:
      return "Confirmed";
    case FleetNodeEnrollmentStatus.REVOKED:
      return "Revoked";
    default:
      return "Unknown";
  }
};

export const enrollmentStatusTone = (status: FleetNodeEnrollmentStatus): StatusKey => {
  switch (status) {
    case FleetNodeEnrollmentStatus.CONFIRMED:
      return statuses.normal;
    case FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION:
    case FleetNodeEnrollmentStatus.PENDING:
      return statuses.pending;
    case FleetNodeEnrollmentStatus.REVOKED:
      return statuses.error;
    default:
      return statuses.inactive;
  }
};

export const isConnected = (lastSeenSeconds: bigint | undefined, now: number = Date.now()): boolean => {
  if (lastSeenSeconds === undefined) return false;
  return now - Number(lastSeenSeconds) * 1000 < CONNECTED_WINDOW_MS;
};

export const pairingStatusLabel = (status: PairingStatus): string => {
  switch (status) {
    case PairingStatus.PAIRED:
      return "Paired";
    case PairingStatus.AUTHENTICATION_NEEDED:
      return "Auth needed";
    case PairingStatus.FAILED:
      return "Failed";
    case PairingStatus.PENDING:
      return "Pairing…";
    default:
      return "Unknown";
  }
};

export const pairingStatusTone = (status: PairingStatus): StatusKey => {
  switch (status) {
    case PairingStatus.PAIRED:
      return statuses.normal;
    case PairingStatus.AUTHENTICATION_NEEDED:
    case PairingStatus.PENDING:
      return statuses.pending;
    case PairingStatus.FAILED:
      return statuses.error;
    default:
      return statuses.inactive;
  }
};
