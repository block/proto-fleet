import React from "react";

import type { FirmwareUpdateInfo } from "@/protoOS/store/contract";
import { SettingsSolid, Stop, Success } from "@/shared/assets/icons";
import { ButtonProps } from "@/shared/components/ButtonGroup";
import Dialog from "@/shared/components/Dialog";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface FirmwareUpdateStatusModalProps {
  updateStatus?: FirmwareUpdateInfo;
  onReboot?: () => void;
  onUpdate?: () => void;
  onDismiss?: () => void;
  rebootPending?: boolean;
  updatePending?: boolean;
  open?: boolean;
}

type StatusConfig = {
  title: string;
  icon?: React.ReactNode;
  statusIndicator: string;
  message?: string;
  getButtons: (props: {
    onUpdate?: () => void;
    onDismiss?: () => void;
    onReboot?: () => void;
    updatePending?: boolean;
    rebootPending?: boolean;
  }) => ButtonProps[] | undefined;
};

const UPDATE_STATUS_CONFIG: Record<string, StatusConfig> = {
  unknown: {
    title: "Unknown status",
    icon: <Stop className="text-text-critical" />,
    statusIndicator: "unknown",
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
    statusIndicator: "checking",
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
    statusIndicator: "downloading",
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
    getButtons: ({ onUpdate, onDismiss, updatePending }) => [
      {
        text: "Dismiss",
        variant: "secondary",
        onClick: onDismiss,
      },
      {
        text: "Install",
        variant: "primary",
        loading: updatePending,
        onClick: onUpdate,
      },
    ],
  },
  installing: {
    title: "Installing update",
    statusIndicator: "installing",
    getButtons: ({ onDismiss }) => [
      {
        text: "Dismiss",
        variant: "secondary",
        onClick: onDismiss,
      },
    ],
  },
  rebootRequired: {
    title: "Update installed",
    icon: <Success className="text-intent-success-fill" />,
    statusIndicator: "installed",
    getButtons: ({ onDismiss, onReboot, rebootPending }) => [
      {
        text: "Dismiss",
        variant: "secondary",
        onClick: onDismiss,
      },
      {
        text: "Reboot now",
        variant: "primary",
        testId: "firmware-status-modal-reboot-button",
        loading: rebootPending,
        onClick: onReboot,
      },
    ],
  },
  rebooting: {
    title: "Rebooting miner",
    statusIndicator: "rebooting",
    message: "Your miner is rebooting. This may take a few minutes.",
    getButtons: () => undefined,
  },
  confirming: {
    title: "Confirming update",
    statusIndicator: "confirming",
    getButtons: ({ onDismiss }) => [
      {
        text: "Dismiss",
        variant: "secondary",
        onClick: onDismiss,
      },
    ],
  },
  complete: {
    title: "Update completed successfully",
    icon: <Success className="text-intent-success-fill" />,
    statusIndicator: "success",
    getButtons: ({ onDismiss }) => [
      {
        text: "Dismiss",
        variant: "secondary",
        onClick: onDismiss,
      },
    ],
  },
  failed: {
    title: "Update error",
    icon: <Stop className="text-text-critical" />,
    statusIndicator: "error",
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
  onUpdate,
  onDismiss,
  rebootPending,
  updatePending,
  open,
}: FirmwareUpdateStatusModalProps) => {
  const getStatusConfig = (): StatusConfig => {
    let status: string = updateStatus?.status || "unknown";

    if (rebootPending && status === "rebootRequired") {
      status = "rebooting";
    }

    return UPDATE_STATUS_CONFIG[status] || UPDATE_STATUS_CONFIG.unknown;
  };

  const statusConfig = getStatusConfig();

  return (
    <Dialog
      open={open}
      testId="firmware-status-modal"
      icon={statusConfig.icon ?? <ProgressCircular indeterminate className="text-core-accent-fill" />}
      title={statusConfig.title}
      buttons={statusConfig.getButtons({
        onUpdate,
        onDismiss,
        onReboot,
        updatePending,
        rebootPending,
      })}
    >
      {updateStatus ? (
        <div className="space-y-2 text-sm">
          <div>{statusConfig.message ?? updateStatus.message}</div>
          {updateStatus.currentVersion ? (
            <div>
              <span className="font-medium">Current Version:</span> {updateStatus.currentVersion}
            </div>
          ) : null}
          {updateStatus.newVersion ? (
            <div>
              <span className="font-medium">New Version:</span> {updateStatus.newVersion}
            </div>
          ) : null}
          {updateStatus.progressPercent !== undefined ? (
            <div>
              <span className="font-medium">Progress:</span> {updateStatus.progressPercent}%
            </div>
          ) : null}
        </div>
      ) : null}
    </Dialog>
  );
};

export default FirmwareUpdateStatusModal;
