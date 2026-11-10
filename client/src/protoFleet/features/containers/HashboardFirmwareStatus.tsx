import { fromJson } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { isCurrentObservation } from "./moduleDetail";
import type { HashboardReading } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { useNow } from "@/protoFleet/features/alerts/lib/useNow";
import ComponentMetadata from "@/shared/components/StatusModal/ComponentMetadata";
import { clamp } from "@/shared/utils/math";

const labels: Record<string, string> = {
  QUEUED: "Queued",
  STOPPING_MINING: "Stopping mining",
  PREFLIGHT: "Checking compatibility",
  FLASHING: "Flashing",
  REBOOTING: "Rebooting",
  VERIFYING: "Verifying",
  RESTORING_MINING: "Restoring mining",
  SUCCEEDED: "Updated",
  SKIPPED_UP_TO_DATE: "Already current",
  FAILED: "Failed",
};

function isCurrentTimestamp(value: unknown, now: number) {
  if (typeof value !== "string") return false;
  try {
    return isCurrentObservation(fromJson(TimestampSchema, value), now);
  } catch {
    return false;
  }
}

export default function HashboardFirmwareStatus({ board }: { board: HashboardReading }) {
  const now = useNow(5000);
  const update = board.metadata?.firmwareUpdate;
  if (!update || typeof update !== "object" || Array.isArray(update)) return null;
  const key = typeof update.status === "string" ? update.status.replace("FIRMWARE_TARGET_STATUS_", "") : undefined;
  const status = key === undefined ? undefined : labels[key];
  if (!status) return null;
  const stale = !isCurrentTimestamp(update.observedAt, now);
  const progress =
    typeof update.progressPercent === "number" && Number.isFinite(update.progressPercent)
      ? Math.round(clamp(update.progressPercent, 0, 100))
      : undefined;
  return (
    <ComponentMetadata
      metadata={{
        update: {
          label: "Firmware update",
          value: `${status}${key === "FLASHING" && progress !== undefined ? ` (${progress}%)` : ""}${stale ? " · Last observed" : ""}`,
        },
        controller: { label: "Controller", value: typeof update.serverId === "string" ? update.serverId : undefined },
        failure: {
          label: "Update failure",
          value:
            typeof update.message === "string"
              ? update.message
              : typeof update.reason === "string"
                ? update.reason
                : undefined,
        },
      }}
    />
  );
}
