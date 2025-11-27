import type { UpdateStatus } from "@/protoOS/api/generatedApi";

export const statusLabelFromUpdateStatus = (updateStatus?: UpdateStatus, verbose: boolean = false) => {
  if (!updateStatus) return undefined;
  switch (updateStatus?.status) {
    case "available":
      return verbose && updateStatus?.new_version
        ? `Update available: ${updateStatus.new_version}`
        : "Update available";
    case "downloading":
      return verbose && updateStatus?.new_version ? `Downloading: ${updateStatus.new_version}` : "Downloading";
    case "downloaded":
      return "Ready to install";
    case "installing":
      return verbose && updateStatus?.new_version ? `Installing: ${updateStatus.new_version}` : "Installing";
    case "installed":
      return "Reboot required";
    case "success":
      return "Update complete";
    case "current":
      return "Firmware up to date";
    case "confirming":
      return "Confirming";
    case "error":
      return "Update failed";
    default:
      return undefined;
  }
};
