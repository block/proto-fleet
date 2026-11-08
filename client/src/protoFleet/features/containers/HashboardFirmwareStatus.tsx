import { useEffect, useState } from "react";
import type { HashboardReading } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import ComponentMetadata from "@/shared/components/StatusModal/ComponentMetadata";

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

export default function HashboardFirmwareStatus({ board }: { board: HashboardReading }) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 5000);
    return () => clearInterval(timer);
  }, []);
  const update = board.metadata?.firmwareUpdate;
  if (!update || typeof update !== "object" || Array.isArray(update)) return null;
  const status =
    typeof update.status === "string" ? labels[update.status.replace("FIRMWARE_TARGET_STATUS_", "")] : undefined;
  if (!status) return null;
  const observed = typeof update.observedAt === "string" ? Date.parse(update.observedAt) : NaN;
  const stale = !Number.isFinite(observed) || now - observed > 120_000 || observed > now + 5_000;
  const progress =
    typeof update.progressPercent === "number" && Number.isFinite(update.progressPercent)
      ? Math.round(Math.min(100, Math.max(0, update.progressPercent)))
      : undefined;
  return (
    <ComponentMetadata
      metadata={{
        update: {
          label: "Firmware update",
          value: `${status}${status === "Flashing" && progress !== undefined ? ` (${progress}%)` : ""}${stale ? " · Last observed" : ""}`,
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
