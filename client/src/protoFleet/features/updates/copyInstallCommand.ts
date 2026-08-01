import { pushToast, STATUSES } from "@/shared/features/toaster";
import { copyToClipboard } from "@/shared/utils/utility";

// Shared by the update notification modal and the settings Updates page so
// both surfaces give identical feedback for the same action.
export const copyInstallCommand = (installCommand: string) => {
  copyToClipboard(installCommand)
    .then(() => {
      pushToast({
        message: "Install command copied to clipboard",
        status: STATUSES.success,
      });
    })
    .catch(() => {
      pushToast({
        message: "Failed to copy install command",
        status: STATUSES.error,
      });
    });
};
