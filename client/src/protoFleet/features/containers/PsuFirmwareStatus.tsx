import type { JsonObject } from "@bufbuild/protobuf";
import { isCurrentJsonObservation } from "./moduleDetail";
import { psuNeedsRecovery } from "./modulePsu";
import ComponentMetadata from "@/shared/components/StatusModal/ComponentMetadata";
import { clamp } from "@/shared/utils/math";

const labels: Record<string, string> = {
  QUEUED: "Queued",
  PREPARING: "Stopping mining",
  STAGING: "Sending firmware",
  VALIDATING: "Checking firmware",
  APPLYING: "Updating",
  RESTORING: "Restoring mining",
  SUCCEEDED: "Updated",
  FAILED: "Failed",
  CANCELLED: "Canceled",
};
const active = ["STAGING", "VALIDATING", "APPLYING"];

/** The module's latest PSU firmware update job, as its controller reports it. */
export default function PsuFirmwareStatus({ update, now }: { update: JsonObject | undefined; now: number }) {
  if (!update) return null;
  const key = typeof update.state === "string" ? update.state.replace("PSU_FIRMWARE_UPDATE_STATE_", "") : undefined;
  const label = key === undefined ? undefined : labels[key];
  if (!label) return null;
  const needsRecovery = psuNeedsRecovery(update);
  const stale = !isCurrentJsonObservation(update.observedAt, now);
  const progress =
    typeof update.progressPercent === "number" && Number.isFinite(update.progressPercent)
      ? Math.round(clamp(update.progressPercent, 0, 100))
      : undefined;
  const status = needsRecovery
    ? "Recovery required"
    : `${label}${active.includes(key!) && progress !== undefined ? ` (${progress}%)` : ""}`;
  return (
    <ComponentMetadata
      metadata={{
        update: { label: "Firmware update", value: `${status}${stale ? " · Last observed" : ""}` },
        controller: { label: "Controller", value: typeof update.serverId === "string" ? update.serverId : undefined },
        failure: {
          label: "Update failure",
          value: typeof update.message === "string" && update.message ? update.message : undefined,
        },
        mining: {
          label: "Mining",
          value:
            update.miningHeld === true
              ? needsRecovery
                ? "Stopped. Send the same PSU firmware again to recover."
                : "Stopped by the update"
              : undefined,
        },
      }}
    />
  );
}
