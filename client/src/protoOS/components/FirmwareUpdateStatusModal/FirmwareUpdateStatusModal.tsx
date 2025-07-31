import React from "react";

import { UpdateStatus } from "@/protoOS/api/types";
import { SettingsSolid, Stop, Success } from "@/shared/assets/icons";
import { ButtonProps } from "@/shared/components/ButtonGroup";
import Dialog from "@/shared/components/Dialog";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface FirmwareUpdateStatusModalProps {
  updateStatus?: UpdateStatus;
  onReboot?: () => void;
  onUpdate?: () => void;
  onContinue?: () => void;
  onDismiss?: () => void;
  show: boolean;
  rebootPending?: boolean;
  updatePending?: boolean;
}

type StatusConfig = {
  title: string;
  icon: React.ReactNode;
  statusIndicator: string;
  message: string;
  getButtons: (props: {
    onUpdate?: () => void;
    onDismiss?: () => void;
    onReboot?: () => void;
    onContinue?: () => void;
    updatePending?: boolean;
    rebootPending?: boolean;
  }) => ButtonProps[];
};

const UPDATE_STATUS_CONFIG: Record<string, StatusConfig> = {
  unknown: {
    title: "Unknown status",
    icon: <Stop className="text-text-critical" />,
    statusIndicator: "unknown",
    message: "No firmware update information available",
    getButtons: ({ onDismiss }) => [
      {
        text: "Dismiss",
        variant: "secondary",
        onClick: onDismiss,
      },
    ],
  },
  checking: {
    title: "Checking for updates",
    icon: <ProgressCircular indeterminate />,
    statusIndicator: "checking",
    message: "Checking for firmware updates",
    getButtons: ({ onDismiss }) => [
      {
        text: "Dismiss",
        variant: "secondary",
        onClick: onDismiss,
      },
    ],
  },
  available: {
    title: "Update available",
    icon: <SettingsSolid />,
    statusIndicator: "available",
    message: "A new firmware version is available for installation",
    getButtons: ({ onUpdate, onDismiss, updatePending }) => [
      {
        text: "Install",
        variant: "primary",
        loading: updatePending,
        onClick: onUpdate,
      },
      {
        text: "Dismiss",
        variant: "secondary",
        onClick: onDismiss,
      },
    ],
  },
  downloading: {
    title: "Downloading update",
    icon: <ProgressCircular indeterminate />,
    statusIndicator: "downloading",
    message: "Downloading firmware update",
    getButtons: ({ onDismiss }) => [
      {
        text: "Dismiss",
        variant: "secondary",
        onClick: onDismiss,
      },
    ],
  },
  downloaded: {
    title: "Ready to install",
    icon: <SettingsSolid />,
    statusIndicator: "downloaded",
    message: "Firmware update downloaded and ready to install",
    getButtons: ({ onDismiss, onContinue }) => [
      {
        text: "Dismiss",
        variant: "secondary",
        onClick: onDismiss,
      },
      {
        text: "Install",
        variant: "primary",
        onClick: onContinue,
      },
    ],
  },
  installing: {
    title: "Installing update",
    icon: <ProgressCircular indeterminate />,
    statusIndicator: "installing",
    message: "Installing firmware update",
    getButtons: ({ onDismiss }) => [
      {
        text: "Dismiss",
        variant: "secondary",
        onClick: onDismiss,
      },
    ],
  },
  installed: {
    title: "Update installed",
    icon: <Success className="text-intent-success-fill" />,
    statusIndicator: "installed",
    message: "Firmware update has been installed successfully",
    getButtons: ({ onDismiss, onReboot, rebootPending }) => [
      {
        text: "Dismiss",
        variant: "secondary",
        onClick: onDismiss,
      },
      {
        text: "Reboot now",
        variant: "primary",
        loading: rebootPending,
        onClick: onReboot,
      },
    ],
  },
  confirming: {
    title: "Confirming update",
    icon: <ProgressCircular indeterminate />,
    statusIndicator: "confirming",
    message: "Confirming firmware update installation",
    getButtons: ({ onDismiss }) => [
      {
        text: "Dismiss",
        variant: "secondary",
        onClick: onDismiss,
      },
    ],
  },
  success: {
    title: "Update completed successfully",
    icon: <Success className="text-intent-success-fill" />,
    statusIndicator: "success",
    message: "Firmware update completed successfully",
    getButtons: ({ onDismiss }) => [
      {
        text: "Dismiss",
        variant: "secondary",
        onClick: onDismiss,
      },
    ],
  },
  error: {
    title: "Update error",
    icon: <Stop className="text-text-critical" />,
    statusIndicator: "error",
    message: "An error occurred during the firmware update",
    getButtons: ({ onDismiss }) => [
      {
        text: "Dismiss",
        variant: "secondary",
        onClick: onDismiss,
      },
    ],
  },
  current: {
    title: "Firmware is up to date",
    icon: <Success className="text-intent-success-fill" />,
    statusIndicator: "current",
    message: "Your firmware is already up to date",
    getButtons: ({ onDismiss }) => [
      {
        text: "Dismiss",
        variant: "secondary",
        onClick: onDismiss,
      },
    ],
  },
};

const FirmwareUpdateStatusModal = ({
  updateStatus,
  onReboot,
  onContinue,
  onUpdate,
  onDismiss,
  show,
  rebootPending,
  updatePending,
}: FirmwareUpdateStatusModalProps) => {
  const getStatusConfig = (): StatusConfig => {
    const status = updateStatus?.status || "unknown";
    return UPDATE_STATUS_CONFIG[status] || UPDATE_STATUS_CONFIG.unknown;
  };

  const statusConfig = getStatusConfig();

  return (
    <Dialog
      show={show}
      icon={statusConfig.icon}
      title={statusConfig.title}
      titleSize="text-heading-300"
      buttons={statusConfig.getButtons({
        onUpdate,
        onDismiss,
        onReboot,
        onContinue,
        updatePending,
        rebootPending,
      })}
    >
      {updateStatus && (
        <div className="space-y-2 text-sm">
          <div>{updateStatus.message}</div>
          {updateStatus.current_version && (
            <div>
              <span className="font-medium">Current Version:</span>{" "}
              {updateStatus.current_version}
            </div>
          )}
          {updateStatus.new_version && (
            <div>
              <span className="font-medium">New Version:</span>{" "}
              {updateStatus.new_version}
            </div>
          )}
          {updateStatus.progress !== undefined && (
            <div>
              <span className="font-medium">Progress:</span>{" "}
              {updateStatus.progress}%
            </div>
          )}
        </div>
      )}
    </Dialog>
  );
};

export default FirmwareUpdateStatusModal;
