import type { OperationTarget } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";

type Result = Pick<OperationTarget, "state" | "resultCode">;

export const poolUpdateStatus = ({ state, resultCode }: Result): string => {
  if (state === "superseded") return "Replaced by a newer update";
  if (state === "different") return "Settings differ";
  if (resultCode === "write_uncertain") return "Not verified";
  if (resultCode === "interrupted") return "Update failed";
  return (
    {
      adopted: "Not verified",
      device_configuration: "Device configuration",
      container_configuration: "Container configuration",
      pending: "Updating…",
      applying: "Updating…",
      verified: "Verified",
      applied_unverified: "Not verified",
      different: "Settings differ",
      failed: "Update failed",
      "": "—",
    }[state] ?? "Unavailable"
  );
};

export const poolStatusIsProblem = (status: string) =>
  ["Update failed", "Not verified", "Settings differ", "Unavailable", "Needs attention"].includes(status);

export const poolStatusExplanation = ({ state, resultCode }: Result): string => {
  if (state === "superseded") return "A newer pool configuration update replaced this update.";
  if (state === "different") return "The device’s reported pool settings don’t match this configuration.";
  if (state === "container_configuration")
    return "Mining groups are not enabled on this container. These hashboards use the whole container’s pool configuration.";
  if (state === "device_configuration")
    return "These pool settings are configured on the device. No Fleet pool configuration is assigned.";
  if (resultCode === "write_uncertain") return "Fleet couldn’t confirm that an earlier device update finished.";
  if (resultCode === "interrupted") return "The update was interrupted before completion.";
  if (resultCode === "check_unavailable") return "Fleet could not read the equipment’s current pool settings.";
  if (state === "failed")
    return (
      {
        unavailable: "Fleet could not access the equipment to apply this configuration.",
        rejected: "Permissions, ownership, or equipment changed before this update could be applied.",
        unsupported: "This equipment does not support the configuration.",
      }[resultCode] ?? "Fleet could not complete this pool configuration update."
    );
  return (
    {
      adopted: "This configuration was imported from existing settings and has not been verified.",
      pending: "This update is waiting to start.",
      applying: "Applying this configuration to the equipment.",
      verified: "The equipment’s reported pool settings matched this configuration when last checked.",
      applied_unverified: "The equipment accepted the update, but Fleet could not verify its pool settings.",
      different: "The equipment’s reported pool settings do not match this configuration.",
      "": "No Fleet pool configuration is assigned, and the device reports no pools.",
    }[state] ?? "Fleet could not load the update status. The update may still be running."
  );
};
