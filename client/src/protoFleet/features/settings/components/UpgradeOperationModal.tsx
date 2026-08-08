import {
  type ReleaseInfo,
  type UpgradeOperation,
  UpgradePhase,
} from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { Copy } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
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
  release?: ReleaseInfo;
  targetVersion?: string;
  triggerError: string | null;
  triggering: boolean;
}

const ACTIVE_PHASE_LABELS: Partial<Record<UpgradePhase, string>> = {
  [UpgradePhase.QUEUED]: "Queued",
  [UpgradePhase.DOWNLOADING]: "Downloading",
  [UpgradePhase.VERIFYING]: "Verifying",
  [UpgradePhase.STAGING]: "Staging",
  [UpgradePhase.PREFLIGHT]: "Preflight",
  [UpgradePhase.ACTIVATING]: "Activating",
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
  release,
  targetVersion,
  triggerError,
  triggering,
}: UpgradeOperationModalProps) => {
  if (!release && !operation && !reconciling && !triggerError) {
    return null;
  }

  const displayedTargetVersion = operation?.targetVersion || targetVersion || release?.version;
  const failed = operation?.phase === UpgradePhase.FAILED;
  const succeeded = operation?.phase === UpgradePhase.SUCCEEDED;
  const active = Boolean(operation && !failed && !succeeded);
  const activePhaseLabel = operation ? (ACTIVE_PHASE_LABELS[operation.phase] ?? "Starting") : undefined;
  const error = operation?.error.trim() ?? "";
  const hostLogPath = operation?.hostLogPath.trim() ?? "";
  const recoveryCommand = operation?.recoveryCommand.trim() ?? "";

  const handleUpgrade = () => {
    if (!release || reconciling) return;
    void onUpgrade(release.version).catch(() => {
      // The route owns reconciliation and exposes a terminal triggerError.
    });
  };

  const handleCopyRecoveryCommand = () => {
    if (!recoveryCommand) return;
    void copyToClipboard(recoveryCommand)
      .then(() => {
        pushToast({
          message: "Recovery command copied to clipboard",
          status: STATUSES.success,
        });
      })
      .catch(() => {
        pushToast({
          message: "Failed to copy recovery command",
          status: STATUSES.error,
        });
      });
  };

  const buttons = manualFallbackReady
    ? [
        {
          text: "I confirmed — unlock manual install",
          variant: variants.secondaryDanger,
          onClick: onUseManualFallback,
          dismissModalOnClick: false,
        },
      ]
    : succeeded
      ? [
          {
            text: "Reload Fleet",
            variant: variants.primary,
            onClick: onReload,
            dismissModalOnClick: false,
          },
        ]
      : failed
        ? [
            {
              text: "Dismiss failure",
              variant: variants.secondary,
              onClick: onAcknowledge,
              dismissModalOnClick: false,
            },
          ]
        : !active && !reconciling && release
          ? [
              {
                text: "Cancel",
                variant: variants.secondary,
                onClick: onDismiss,
                dismissModalOnClick: false,
              },
              {
                text: `Confirm upgrade to ${release.version}`,
                variant: variants.primary,
                onClick: handleUpgrade,
                loading: triggering,
                dismissModalOnClick: false,
              },
            ]
          : undefined;

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      title={displayedTargetVersion ? `Upgrade Fleet to ${displayedTargetVersion}` : "Upgrade Fleet"}
      divider={false}
      testId="upgrade-operation-modal"
      buttons={buttons}
    >
      <div className="flex flex-col gap-5">
        {reconciling ? (
          <div
            role="status"
            aria-live="polite"
            aria-atomic="true"
            className="flex flex-col gap-3 rounded-xl bg-core-primary-5 p-5"
          >
            <div className="flex items-center gap-3">
              <span aria-hidden="true">
                <ProgressCircular indeterminate size={20} />
              </span>
              <div className="text-heading-100 text-text-primary">
                {manualFallbackReady ? "Fleet could not confirm the upgrade outcome" : "Checking upgrade status"}
              </div>
            </div>
            {manualFallbackReady ? (
              <p className="text-300 font-medium text-text-critical">
                The host updater is not reporting this upgrade. Only unlock the manual command after checking the host
                and confirming no upgrade is running. Overlapping installs can leave the deployment unusable.
              </p>
            ) : (
              <p className="text-300 text-text-primary-70">
                Fleet is reconciling the tracked upgrade with the host updater. Do not run the manual install command
                yet; wait until this check finishes.
              </p>
            )}
          </div>
        ) : active ? (
          <div
            role="status"
            aria-live="polite"
            aria-atomic="true"
            className="flex flex-col gap-3 rounded-xl bg-core-primary-5 p-5"
          >
            <div className="flex items-center gap-3">
              <span aria-hidden="true">
                <ProgressCircular indeterminate size={20} />
              </span>
              <div className="text-heading-100 text-text-primary">{operation?.message || "Upgrade in progress"}</div>
            </div>
            <div className="text-200 text-text-primary-70">Phase: {activePhaseLabel}</div>
            {connectionLost ? (
              <p className="text-300 text-text-primary-70">
                Fleet is temporarily unreachable. A disconnect is expected while services restart; this page will keep
                checking for progress.
              </p>
            ) : (
              <p className="text-300 text-text-primary-70">
                You can close this dialog while Fleet downloads, validates, and activates the release. Return to this
                Updates page to check progress.
              </p>
            )}
          </div>
        ) : failed ? (
          <div role="alert" className="flex flex-col gap-3 rounded-xl bg-intent-critical-10 p-5">
            <div className="text-heading-100 text-text-primary">{operation?.message || "Upgrade failed"}</div>
            {error ? <p className="text-300 text-text-critical">{error}</p> : null}
            {hostLogPath ? (
              <p className="text-200 text-text-primary-70">
                Host log: <code className="font-mono break-all">{hostLogPath}</code>
              </p>
            ) : null}
            {recoveryCommand ? (
              <div className="flex flex-col gap-2">
                <div className="text-200 text-text-primary-70">Recovery command</div>
                <div className="flex items-center justify-between gap-2 rounded-xl bg-surface-default px-4 py-3">
                  <code className="min-w-0 flex-1 font-mono text-200 break-all text-text-primary">
                    {recoveryCommand}
                  </code>
                  <Button
                    ariaLabel="Copy recovery command"
                    variant={variants.ghost}
                    prefixIcon={<Copy width="w-4" />}
                    onClick={handleCopyRecoveryCommand}
                    className="shrink-0"
                  />
                </div>
              </div>
            ) : null}
          </div>
        ) : succeeded ? (
          <div
            role="status"
            aria-live="polite"
            aria-atomic="true"
            className="flex flex-col gap-3 rounded-xl bg-core-primary-5 p-5"
          >
            <div className="text-heading-100 text-text-primary">{operation?.message || "Upgrade complete"}</div>
            <p className="text-300 text-text-primary-70">
              Reload Fleet to use the client bundled with {operation?.targetVersion}.
            </p>
          </div>
        ) : release ? (
          <div className="flex flex-col gap-3 rounded-xl bg-intent-warning-10 p-5">
            <div className="text-heading-100 text-text-primary">Confirm upgrade to {release.version}</div>
            <p className="text-300 text-text-primary-70">
              Fleet will validate and build this exact release first, then take the instance offline for several minutes
              while containers restart and database migrations run.
            </p>
            {release.prerelease ? (
              <p className="text-300 font-medium text-text-critical">
                This is a release candidate. The upgrade can run forward-only database migrations, and you cannot
                downgrade this instance afterward. Continue only if you accept that recovery may require a newer
                compatible release.
              </p>
            ) : null}
          </div>
        ) : null}

        {triggerError ? (
          <p role="alert" className="text-300 text-text-critical">
            {triggerError}
          </p>
        ) : null}
      </div>
    </Modal>
  );
};

export default UpgradeOperationModal;
