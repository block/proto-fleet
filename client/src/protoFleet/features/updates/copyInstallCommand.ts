import { pushToast, STATUSES } from "@/shared/features/toaster";
import { copyToClipboard } from "@/shared/utils/utility";

// Keep the manual fallback's clipboard feedback consistent wherever the
// Settings update flow renders it.
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
