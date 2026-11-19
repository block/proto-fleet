import type { OperationTarget } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";

type Result = Pick<OperationTarget, "state" | "resultCode">;

export const poolUpdateStatus = ({ state, resultCode }: Result): string => {
  if (state === "superseded") return "Replaced by a newer update";
  if (state === "different") return "Settings differ";
  if (resultCode === "write_uncertain") return "Not verified";
  if (resultCode === "interrupted") return "Update failed";
  return (
    {
      cleared: "—",
      recoverable: "Recovery available",
      placement_pending: "Assignment pending",
      uncertain: "Not verified",
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
  [
    "Assignment pending",
    "Update failed",
    "Not verified",
    "Settings differ",
    "Unavailable",
    "Needs attention",
    "Recovery available",
  ].includes(status);

export const poolStatusExplanation = ({ state, resultCode }: Result): string => {
  if (state === "placement_pending")
    return "Fleet preserved this board’s pool assignment. Its location or ownership must be confirmed before pool changes are allowed. Other mapped boards can be updated.";
  if (state === "recoverable")
    return "Fleet retained this container’s pool assignment because board placement is incomplete. An authorized operator can choose Remove assignment, then Keep device settings, to remove the Fleet assignment without changing device pools.";
  if (state === "cleared") return "Stored and runtime pool settings were verified as cleared.";
  if (resultCode === "preparation_unavailable")
    return "Waiting for equipment readiness before applying its owner’s pool configuration.";
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

// Removal results are historical action outcomes, not verification of the old
// assigned configuration. They must never offer the normal reapply flow.
export const poolRemovalOutcome = (state: string, keep: boolean, resultCode?: string) => {
  if (["applying", "uncertain"].includes(state)) {
    if (resultCode === "operator_recovery_required")
      return {
        status: "Administrator recovery required",
        message:
          "Fleet cannot confirm whether the earlier device command has finished. Contact an administrator to recover this operation before changing pools.",
      };
    if (resultCode === "runtime_pools_remain")
      return {
        status: "Runtime pools still present",
        message:
          "Saved pools are cleared, but the equipment still reports runtime pools. Fleet will keep checking. The Fleet assignment remains until verification succeeds.",
      };
    if (resultCode === "clear_verification_unavailable")
      return {
        status: "Waiting for verification",
        message:
          "Fleet cannot verify that the hashboard’s runtime pools are cleared. Fleet will keep checking. The Fleet assignment remains until verification succeeds.",
      };
  }
  if (state === "completed")
    return {
      status: keep ? "Assignment removed" : "—",
      message: keep
        ? "Fleet assignment removed. Device pool settings were preserved."
        : "Device pools cleared and Fleet assignment removed.",
    };
  if (state === "failed")
    return {
      status: "Removal failed",
      message: "Removal failed. The Fleet assignment was kept. Review the device’s settings before trying again.",
    };
  if (state === "superseded")
    return {
      status: "Replaced by a newer update",
      message: "A newer pool configuration update replaced this removal.",
    };
  if (state === "uncertain")
    return {
      status: "Removal not confirmed",
      message: "Fleet has not confirmed removal. Check its result before another pool change.",
    };
  return {
    status: "Removing pools…",
    message: "Clearing pools and verifying removal. The Fleet assignment stays until verification completes.",
  };
};
