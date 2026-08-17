import {
  type ReleaseInfo,
  type UpgradeOperation,
  UpgradePhase,
} from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { Alert, LogoAlt, Stop, Success } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import type { ButtonProps } from "@/shared/components/ButtonGroup";
import Callout from "@/shared/components/Callout";
import Dialog from "@/shared/components/Dialog";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { copyToClipboard } from "@/shared/utils/utility";

export interface UpgradeOperationModalProps {
  connectionLost: boolean;
  manualFallbackReady: boolean;
  onAcknowledge: () => void;
  onDismiss: () => void;
  onReload: () => void;
  onUpgrade: (targetVersion: string) => Promise<void>;
  onUseManualFallback: () => void;
  open: boolean;
  operation?: UpgradeOperation;
  reconciling: boolean;
  reloadPending?: boolean;
  release?: ReleaseInfo;
  targetVersion?: string;
  triggerError: string | null;
  triggering: boolean;
}

const ReconciliationPanel = () => (
  <div role="alert" aria-live="polite" aria-atomic="true" className="space-y-3">
    <p className="text-300 text-text-primary">
      We couldn't confirm whether the update started. Check the host and make sure no update is running before you use
      manual install.
    </p>
  </div>
);

interface ActiveUpgradePanelProps {
  connectionLost: boolean;
}

const ActiveUpgradePanel = ({ connectionLost }: ActiveUpgradePanelProps) => (
  <div role="status" aria-live="polite" aria-atomic="true" className="space-y-3">
    {connectionLost ? (
      <p className="text-300 text-text-primary-70">Fleet will reconnect after services restart.</p>
    ) : (
      <p className="text-300 text-text-primary-70">You can close this dialog while the update runs.</p>
    )}
  </div>
);

const copyRecoveryCommand = (recoveryCommand: string) => {
  void copyToClipboard(recoveryCommand)
    .then(() => {
      pushToast({
        message: "Recovery command copied to clipboard",
        status: STATUSES.success,
      });
    })
    .catch(() => {
      pushToast({
        message: "Couldn't copy recovery command",
        status: STATUSES.error,
      });
    });
};

const FailedUpgradePanel = ({ operation }: { operation: UpgradeOperation }) => {
  const error = operation.error.trim();
  const hostLogPath = operation.hostLogPath.trim();
  const recoveryCommand = operation.recoveryCommand.trim();

  return (
    <div role="alert" className="flex flex-col gap-3">
      {recoveryCommand ? (
        <>
          <p className="text-300 text-text-primary-70">Run this command on the Fleet host to continue the update.</p>
          <code className="rounded-xl bg-surface-default px-4 py-3 font-mono text-200 break-all text-text-primary">
            {recoveryCommand}
          </code>
        </>
      ) : error ? (
        <p className="text-300 text-text-primary">{error}</p>
      ) : null}
      {!recoveryCommand && hostLogPath ? (
        <p className="text-200 text-text-primary-70">
          Host log: <code className="font-mono break-all">{hostLogPath}</code>
        </p>
      ) : null}
    </div>
  );
};

const SucceededUpgradePanel = ({ operation }: { operation: UpgradeOperation }) => (
  <div role="status" aria-live="polite" aria-atomic="true">
    <p className="text-300 text-text-primary-70">Relaunch Fleet to finish updating to {operation.targetVersion}.</p>
  </div>
);

const UpgradeConfirmationPanel = ({ release }: { release: ReleaseInfo }) => (
  <div className="space-y-3">
    <p className="text-300 text-text-primary-70">
      Fleet validates this release before restarting. The instance will be offline for a few minutes while services
      restart and database migrations run.
    </p>
    {release.prerelease ? (
      <Callout
        intent="danger"
        prefixIcon={<Alert />}
        title="You can't return to an earlier release after this update."
      />
    ) : null}
  </div>
);

interface UpgradeOperationContentProps {
  connectionLost: boolean;
  manualFallbackReady: boolean;
  operation?: UpgradeOperation;
  reconciling: boolean;
  release?: ReleaseInfo;
}

const UpgradeOperationContent = ({
  connectionLost,
  manualFallbackReady,
  operation,
  reconciling,
  release,
}: UpgradeOperationContentProps) => {
  if (reconciling) {
    return manualFallbackReady ? <ReconciliationPanel /> : null;
  }
  if (!operation) {
    return release ? <UpgradeConfirmationPanel release={release} /> : null;
  }
  if (operation.phase === UpgradePhase.FAILED) {
    return <FailedUpgradePanel operation={operation} />;
  }
  if (operation.phase === UpgradePhase.SUCCEEDED) {
    return <SucceededUpgradePanel operation={operation} />;
  }
  return <ActiveUpgradePanel connectionLost={connectionLost} />;
};

interface GetModalButtonsOptions {
  handleUpgrade: () => void;
  manualFallbackReady: boolean;
  onAcknowledge: () => void;
  onDismiss: () => void;
  onReload: () => void;
  onUseManualFallback: () => void;
  operation?: UpgradeOperation;
  reconciling: boolean;
  reloadPending: boolean;
  release?: ReleaseInfo;
  triggering: boolean;
}

const getModalButtons = ({
  handleUpgrade,
  manualFallbackReady,
  onAcknowledge,
  onDismiss,
  onReload,
  onUseManualFallback,
  operation,
  reconciling,
  reloadPending,
  release,
  triggering,
}: GetModalButtonsOptions): ButtonProps[] | undefined => {
  if (manualFallbackReady) {
    return [
      {
        text: "Use manual install",
        variant: variants.secondaryDanger,
        onClick: onUseManualFallback,
      },
      {
        text: "Close",
        variant: variants.secondary,
        onClick: onDismiss,
      },
    ];
  }
  if (operation?.phase === UpgradePhase.SUCCEEDED) {
    return [
      {
        text: "Relaunch",
        variant: variants.primary,
        onClick: onReload,
        loading: reloadPending,
        dismissModalOnClick: false,
      },
    ];
  }
  if (operation?.phase === UpgradePhase.FAILED) {
    const recoveryCommand = operation.recoveryCommand.trim();
    return [
      ...(recoveryCommand
        ? [
            {
              text: "Copy recovery command",
              variant: variants.primary,
              onClick: () => copyRecoveryCommand(recoveryCommand),
            },
          ]
        : []),
      {
        text: "Close",
        variant: variants.secondary,
        onClick: onAcknowledge,
      },
    ];
  }
  if (operation || reconciling) {
    return [
      {
        text: "Dismiss",
        variant: variants.secondary,
        onClick: onDismiss,
      },
    ];
  }
  if (!release) {
    return [
      {
        text: "Close",
        variant: variants.secondary,
        onClick: onDismiss,
      },
    ];
  }
  return [
    {
      text: "Cancel",
      variant: variants.secondary,
      onClick: onDismiss,
    },
    {
      text: "Update now",
      variant: variants.primary,
      onClick: handleUpgrade,
      loading: triggering,
    },
  ];
};

const getDialogVisual = ({
  manualFallbackReady,
  operation,
  reconciling,
  release,
  targetVersion,
}: Pick<
  UpgradeOperationModalProps,
  "manualFallbackReady" | "operation" | "reconciling" | "release" | "targetVersion"
>) => {
  if (reconciling) {
    const trackedVersion = operation?.targetVersion || targetVersion;
    if (manualFallbackReady) {
      return {
        icon: <Stop className="text-text-critical" />,
        title: "Manual install available",
      };
    }
    return {
      icon: <ProgressCircular indeterminate />,
      title: trackedVersion ? `Checking update to ${trackedVersion}` : "Checking update status",
    };
  }

  if (!operation) {
    return {
      icon: <LogoAlt width="w-5" testId="upgrade-dialog-icon" />,
      title: release ? `Update Fleet to ${release.version}` : "Update Fleet",
    };
  }

  if (operation.phase === UpgradePhase.FAILED) {
    return {
      icon: <Stop className="text-text-critical" />,
      title: operation.recoveryCommand.trim() ? "Update needs recovery" : "Update couldn't complete",
    };
  }
  if (operation.phase === UpgradePhase.SUCCEEDED) {
    return {
      icon: <Success className="text-intent-success-fill" />,
      title: "Update complete",
    };
  }
  return {
    icon: <ProgressCircular indeterminate />,
    title: operation.message || `Updating Fleet to ${operation.targetVersion || targetVersion}`,
  };
};

const UpgradeOperationModal = ({
  connectionLost,
  manualFallbackReady,
  onAcknowledge,
  onDismiss,
  onReload,
  onUpgrade,
  onUseManualFallback,
  open,
  operation,
  reconciling,
  reloadPending = false,
  release,
  targetVersion,
  triggerError,
  triggering,
}: UpgradeOperationModalProps) => {
  if (reconciling && !manualFallbackReady) {
    return null;
  }
  if (!release && !operation && !reconciling && !triggerError) {
    return null;
  }

  const handleUpgrade = () => {
    if (!release || reconciling) return;
    void onUpgrade(release.version).catch(() => {
      // The route owns reconciliation and exposes a terminal triggerError.
    });
  };

  const buttons = getModalButtons({
    handleUpgrade,
    manualFallbackReady,
    onAcknowledge,
    onDismiss,
    onReload,
    onUseManualFallback,
    operation,
    reconciling,
    reloadPending,
    release,
    triggering,
  });
  const dialogVisual = getDialogVisual({
    manualFallbackReady,
    operation,
    reconciling,
    release,
    targetVersion,
  });
  const showTriggerError = triggerError && !manualFallbackReady && operation?.phase !== UpgradePhase.FAILED;

  return (
    <Dialog
      open={open}
      onDismiss={onDismiss}
      testId="upgrade-operation-modal"
      icon={dialogVisual.icon}
      title={dialogVisual.title}
      buttons={buttons}
    >
      <div className="flex flex-col gap-5">
        <UpgradeOperationContent
          connectionLost={connectionLost}
          manualFallbackReady={manualFallbackReady}
          operation={operation}
          reconciling={reconciling}
          release={release}
        />

        {showTriggerError ? (
          <div role="alert">
            <Callout intent="danger" prefixIcon={<Alert />} title={triggerError} />
          </div>
        ) : null}
      </div>
    </Dialog>
  );
};

export default UpgradeOperationModal;
