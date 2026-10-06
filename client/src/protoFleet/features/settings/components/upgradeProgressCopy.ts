import { UpgradePhase } from "@/protoFleet/api/generated/instance/v1/updates_pb";

export const getUpgradeProgressCopy = (phase: UpgradePhase) => {
  switch (phase) {
    case UpgradePhase.QUEUED:
    case UpgradePhase.STAGING:
      return "Preparing update";
    case UpgradePhase.DOWNLOADING:
      return "Downloading update";
    case UpgradePhase.VERIFYING:
    case UpgradePhase.PREFLIGHT:
      return "Checking update";
    case UpgradePhase.ACTIVATING:
      return "Restarting Fleet";
    default:
      return "Updating Fleet";
  }
};
