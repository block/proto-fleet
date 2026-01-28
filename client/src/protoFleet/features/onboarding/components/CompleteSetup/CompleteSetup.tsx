import { ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { DeviceStatus, PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { StreamCommandBatchUpdatesRequestSchema } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import useAuthNeededMiners from "@/protoFleet/api/useAuthNeededMiners";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import usePoolNeededCount from "@/protoFleet/api/usePoolNeededCount";
import { AuthenticateMiners } from "@/protoFleet/features/auth/components/AuthenticateMiners";
import PoolSelectionPageWrapper from "@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage";
import { useLastPairingCompletedAt } from "@/protoFleet/store";
import { Alert, Dismiss, MiningPools } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import { pushToast, STATUSES as TOAST_STATUSES, updateToast } from "@/shared/features/toaster";
import { useReactiveLocalStorage } from "@/shared/hooks/useReactiveLocalStorage";

type TaskCardProps = {
  icon: ReactNode;
  title: string;
  description?: string;
  actionText?: string;
  onActionClick?: () => void;
  skippable?: boolean;
  onSkip?: () => void;
  isLoading?: boolean;
};

const TaskCard = ({
  icon,
  title,
  description,
  actionText,
  onActionClick,
  skippable = false,
  onSkip,
  isLoading = false,
}: TaskCardProps) => {
  return (
    <div className="flex flex-col justify-between gap-4 rounded-2xl bg-surface-base p-6">
      <div className="flex flex-col gap-4">
        <div className="flex size-8 items-center justify-center rounded-lg bg-surface-5">{icon}</div>
        <div className="flex flex-col">
          <div className="text-emphasis-300">{title}</div>
          {description && <div className="text-300">{description}</div>}
        </div>
      </div>
      <div className="flex justify-between gap-5">
        {skippable && (
          <Button className="pl-0" variant="textOnly" onClick={onSkip} disabled={isLoading}>
            Skip
          </Button>
        )}
        <Button
          onClick={onActionClick}
          variant={skippable ? "secondary" : "primary"}
          className={skippable ? "" : "w-full"}
          disabled={isLoading}
          loading={isLoading}
        >
          {actionText}
        </Button>
      </div>
    </div>
  );
};

const AuthenticateMinersCard = ({
  count,
  onAuthenticationSuccess,
}: {
  count: number;
  onAuthenticationSuccess: () => void;
}) => {
  const [showAuthMinersModal, setShowAuthMinersModal] = useState(false);

  return (
    <>
      <TaskCard
        icon={<Alert className="text-text-critical" />}
        title="Authenticate miners"
        description={`${count} miner${count === 1 ? "" : "s"} ${count === 1 ? "needs" : "need"} attention`}
        actionText="Authenticate"
        onActionClick={() => setShowAuthMinersModal(true)}
      />
      {showAuthMinersModal && (
        <AuthenticateMiners onClose={() => setShowAuthMinersModal(false)} onSuccess={onAuthenticationSuccess} />
      )}
    </>
  );
};

const ConfigurePoolCard = ({
  count,
  onConfigureClick,
  isLoading,
}: {
  count: number;
  onConfigureClick: () => void;
  isLoading: boolean;
}) => {
  const [configurePoolDismissed, setConfigurePoolDismissed] =
    useReactiveLocalStorage<boolean>("configurePoolDismissed");

  if (configurePoolDismissed) {
    return null;
  }

  return (
    <TaskCard
      icon={<MiningPools className="text-text-primary" />}
      title="Configure pools"
      description={`${count} ${count === 1 ? "miner" : "miners"}`}
      actionText="Configure"
      onActionClick={onConfigureClick}
      skippable
      onSkip={() => setConfigurePoolDismissed(true)}
      isLoading={isLoading}
    />
  );
};

type CompleteSetupProps = {
  className?: string;
};

const CompleteSetup = ({ className = "" }: CompleteSetupProps) => {
  const [completSetupDismissed, setCompletSetupDismissed] = useReactiveLocalStorage<boolean>("completeSetupDismissed");

  const handleDismiss = () => {
    setCompletSetupDismissed(true);
  };

  // Fetch miners needing authentication to show in the "Authenticate miners" card
  const { totalMiners: authNeededCount, refetch: refetchAuthNeededMiners } = useAuthNeededMiners({
    pageSize: 100,
  });

  // Fetch count of miners needing pool configuration
  const { poolNeededCount, isLoading: isLoadingPoolNeeded, refetch: refetchPoolNeededCount } = usePoolNeededCount();

  // Get streaming command batch updates
  const { streamCommandBatchUpdates } = useMinerCommand();

  // State for showing pool selection modal
  const [showPoolSelectionModal, setShowPoolSelectionModal] = useState(false);

  // State for tracking when we're polling after pool assignment
  const [isPollingAfterPoolAssignment, setIsPollingAfterPoolAssignment] = useState(false);

  // Store cleanup function to stop polling when status is detected
  const pollingCleanupRef = useRef<(() => void) | null>(null);
  // Track pool count when polling starts to detect changes
  const poolCountWhenPollingStartedRef = useRef<number | null>(null);

  // Reusable polling logic with exponential backoff
  // Returns cleanup function to cancel pending polls
  const pollForStatusUpdates = useCallback(() => {
    setIsPollingAfterPoolAssignment(true);
    // Capture the current pool count when polling starts
    poolCountWhenPollingStartedRef.current = poolNeededCount;

    let pollCount = 0;
    const maxPolls = 6;
    const timeouts: ReturnType<typeof setTimeout>[] = [];
    let cancelled = false;

    const poll = () => {
      if (cancelled) {
        return;
      }

      refetchAuthNeededMiners();
      refetchPoolNeededCount();

      pollCount += 1;

      if (pollCount < maxPolls) {
        // Exponential backoff: 500ms, 1s, 2s, 4s, 8s (5 delays for 6 total polls)
        const delay = 500 * Math.pow(2, pollCount - 1);

        const timeoutId = setTimeout(() => {
          poll();
        }, delay);

        timeouts.push(timeoutId);
      }
    };

    poll();

    // Return cleanup function
    const cleanup = () => {
      cancelled = true;
      timeouts.forEach((id) => clearTimeout(id));
      pollingCleanupRef.current = null;
      poolCountWhenPollingStartedRef.current = null;
      setIsPollingAfterPoolAssignment(false);
    };

    pollingCleanupRef.current = cleanup;
    return cleanup;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- poolNeededCount intentionally omitted to prevent stale closure; ref-based tracking used instead
  }, [refetchAuthNeededMiners, refetchPoolNeededCount]);

  // Stop polling once we detect pool count has changed from when polling started
  useEffect(() => {
    if (pollingCleanupRef.current && poolCountWhenPollingStartedRef.current !== null) {
      const hasChanged = poolCountWhenPollingStartedRef.current !== poolNeededCount;
      if (hasChanged) {
        pollingCleanupRef.current();
      }
    }
  }, [poolNeededCount]);

  // Ensure polling is cleaned up if the component unmounts while polling is active
  useEffect(() => {
    return () => {
      if (pollingCleanupRef.current) {
        pollingCleanupRef.current();
      }
    };
  }, []);

  // Handlers for pool selection modal
  const handlePoolAssignmentSuccess = useCallback(
    (batchIdentifier: string) => {
      setShowPoolSelectionModal(false);

      const toastId = pushToast({
        message: "Assigning pools to miners",
        status: TOAST_STATUSES.loading,
        longRunning: true,
      });

      const streamAbortController = new AbortController();
      let errorToastId: number | null = null;
      let successCount = 0;
      let totalCount = 0;

      streamCommandBatchUpdates({
        streamRequest: create(StreamCommandBatchUpdatesRequestSchema, {
          batchIdentifier,
        }),
        onStreamData: (response) => {
          totalCount = Number(response.status?.commandBatchDeviceCount?.total || 0);
          successCount = Number(response.status?.commandBatchDeviceCount?.success || 0);

          updateToast(toastId, {
            message: `Assigned pools to ${successCount} out of ${totalCount} miners`,
            status: TOAST_STATUSES.success,
          });

          const failureCount = Number(response.status?.commandBatchDeviceCount?.failure || 0);
          if (failureCount > 0) {
            if (!errorToastId) {
              errorToastId = pushToast({
                message: `Update failed on ${failureCount} out of ${totalCount} miners`,
                status: TOAST_STATUSES.error,
                longRunning: true,
              });
            } else {
              updateToast(errorToastId, {
                message: `Update failed on ${failureCount} out of ${totalCount} miners`,
                status: TOAST_STATUSES.error,
              });
            }
          }
        },
        streamAbortController: streamAbortController,
      }).finally(() => {
        updateToast(toastId, {
          message: `Assigned pools to ${successCount} out of ${totalCount} miners`,
          status: TOAST_STATUSES.success,
        });
        // Start polling to wait for backend to update device status
        pollForStatusUpdates();
      });
    },
    [streamCommandBatchUpdates, pollForStatusUpdates],
  );

  const handlePoolAssignmentError = useCallback((error: string) => {
    pushToast({
      message: error,
      status: TOAST_STATUSES.error,
      longRunning: true,
    });
    setShowPoolSelectionModal(false);
  }, []);

  // Watch for pairing operations completing and start polling
  const lastPairingCompletedAt = useLastPairingCompletedAt();
  const lastProcessedPairingTimestampRef = useRef(0);

  useEffect(() => {
    if (lastPairingCompletedAt > 0 && lastPairingCompletedAt !== lastProcessedPairingTimestampRef.current) {
      lastProcessedPairingTimestampRef.current = lastPairingCompletedAt;
      return pollForStatusUpdates();
    }
    // Note: Intentionally not including pollForStatusUpdates in deps to avoid re-running
    // when refetch functions change. We only want to poll on new pairing completion.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lastPairingCompletedAt]);

  // Track which cards are dismissed to determine if we should show the component
  const [configurePoolDismissed] = useReactiveLocalStorage<boolean>("configurePoolDismissed");

  // Determine which cards are visible (have content and not dismissed)
  const hasConfigurePoolCard = poolNeededCount > 0 && !configurePoolDismissed;
  const hasAuthCard = authNeededCount > 0;

  // Show complete setup banner if:
  // 1. User hasn't explicitly dismissed the entire component AND
  // 2. At least one card is visible
  const shouldShow = !completSetupDismissed && (hasConfigurePoolCard || hasAuthCard);

  return (
    <>
      {shouldShow && (
        <div className={className}>
          <div className="@container rounded-3xl bg-core-primary-5 p-6">
            <div className="mb-6 flex items-center justify-between gap-x-10">
              <div className="text-heading-300">Complete setup</div>
              <Button onClick={handleDismiss} variant="secondary" prefixIcon={<Dismiss />}></Button>
            </div>
            <div className="grid gap-4 @lg:grid-cols-2 @3xl:grid-cols-3 @7xl:grid-cols-4">
              {hasConfigurePoolCard && (
                <ConfigurePoolCard
                  count={poolNeededCount}
                  onConfigureClick={() => {
                    if (poolNeededCount === 0) {
                      return;
                    }

                    setShowPoolSelectionModal(true);
                  }}
                  isLoading={isLoadingPoolNeeded || isPollingAfterPoolAssignment}
                />
              )}
              {hasAuthCard && (
                <AuthenticateMinersCard count={authNeededCount} onAuthenticationSuccess={refetchAuthNeededMiners} />
              )}
            </div>
          </div>
        </div>
      )}
      {showPoolSelectionModal && (
        <PoolSelectionPageWrapper
          selectionMode="all"
          poolNeededCount={poolNeededCount}
          filterCriteria={{
            deviceStatus: DeviceStatus.NEEDS_MINING_POOL,
            pairingStatus: PairingStatus.PAIRED,
          }}
          onSuccess={handlePoolAssignmentSuccess}
          onError={handlePoolAssignmentError}
          onDismiss={() => setShowPoolSelectionModal(false)}
        />
      )}
    </>
  );
};

export default CompleteSetup;
