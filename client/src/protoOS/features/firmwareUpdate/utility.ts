import type { FirmwareUpdateInfo } from "@/protoOS/store/contract";

export const statusLabelFromUpdateStatus = (updateStatus?: FirmwareUpdateInfo, verbose: boolean = false) => {
  if (!updateStatus) return undefined;
  switch (updateStatus?.status) {
    case "available":
      return verbose && updateStatus?.newVersion ? `Update available: ${updateStatus.newVersion}` : "Update available";
    case "downloading":
      return verbose && updateStatus?.newVersion ? `Downloading: ${updateStatus.newVersion}` : "Downloading";
    case "downloaded":
      return "Ready to install";
    case "installing":
      return verbose && updateStatus?.newVersion ? `Installing: ${updateStatus.newVersion}` : "Installing";
    case "rebootRequired":
      return "Reboot required";
    case "complete":
      return "Update complete";
    case "current":
      return "Firmware up to date";
    case "confirming":
      return "Confirming";
    case "failed":
      return "Update failed";
    default:
      return undefined;
  }
};
