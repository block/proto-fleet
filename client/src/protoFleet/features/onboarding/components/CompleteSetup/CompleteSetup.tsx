import { AnimatePresence, motion } from "motion/react";
import { ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { DeviceStatus, PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import useAuthNeededMiners from "@/protoFleet/api/useAuthNeededMiners";
import usePoolNeededCount from "@/protoFleet/api/usePoolNeededCount";
import { AuthenticateMiners } from "@/protoFleet/features/auth/components/AuthenticateMiners";
import PoolSelectionPageWrapper from "@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage";
import { Alert, Dismiss, MiningPools } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
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
    <div className="flex flex-col justify-between gap-4 rounded-2xl bg-surface-overlay p-6">
      <div className="flex flex-col gap-4">
        <div className="flex size-8 items-center justify-center rounded-lg bg-surface-5">{icon}</div>
        <div className="flex flex-col">
          <div className="text-emphasis-300">{title}</div>
          {description ? <div className="text-300">{description}</div> : null}
        </div>
      </div>
      <div className="flex justify-between gap-5">
        {skippable ? (
          <Button className="pl-0" variant="textOnly" onClick={onSkip} disabled={isLoading}>
            Skip
          </Button>
        ) : null}
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
  onRefetchMiners,
  onPairingCompleted,
}: {
  count: number;
  onAuthenticationSuccess: () => void;
  onRefetchMiners?: () => void;
  onPairingCompleted?: () => void;
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
      <AuthenticateMiners
        open={showAuthMinersModal}
        onClose={() => setShowAuthMinersModal(false)}
        onSuccess={onAuthenticationSuccess}
        onRefetchMiners={onRefetchMiners}
        onPairingCompleted={onPairingCompleted}
      />
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
  lastPairingCompletedAt?: number;
  minersChangedAt?: number;
  onRefetchMiners?: () => void;
  onPairingCompleted?: () => void;
};

const CompleteSetup = ({
  className = "",
  lastPairingCompletedAt: externalPairingTimestamp = 0,
  minersChangedAt = 0,
  onRefetchMiners,
  onPairingCompleted: externalOnPairingCompleted,
}: CompleteSetupProps) => {
  // Internal pairing state for callers that don't wire external callbacks (e.g., Dashboard).
  // Uses whichever timestamp is newer: external prop or internal state.
  const [internalPairingTimestamp, setInternalPairingTimestamp] = useState(0);
  const lastPairingCompletedAt = Math.max(externalPairingTimestamp, internalPairingTimestamp);
  const onPairingCompleted = useCallback(() => {
    externalOnPairingCompleted?.();
    setInternalPairingTimestamp(Date.now());
  }, [externalOnPairingCompleted]);
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

  const lastHandledMinersChangedAtRef = useRef(0);
  useEffect(() => {
    if (
      completSetupDismissed ||
      isLoadingPoolNeeded ||
      minersChangedAt <= 0 ||
      minersChangedAt === lastHandledMinersChangedAtRef.current
    ) {
      return;
    }

    lastHandledMinersChangedAtRef.current = minersChangedAt;
    refetchAuthNeededMiners();
    refetchPoolNeededCount();
  }, [completSetupDismissed, isLoadingPoolNeeded, minersChangedAt, refetchAuthNeededMiners, refetchPoolNeededCount]);

  // State for showing pool selection modal
  const [showPoolSelectionModal, setShowPoolSelectionModal] = useState(false);

  // State for tracking when we're polling after pool assignment
  const [isRefreshingEquipment, setIsRefreshingEquipment] = useState(false);

  // Store cleanup function to stop polling when status is detected
  const pollingCleanupRef = useRef<(() => void) | null>(null);
  // Track pool count when polling starts to detect changes
  const poolCountWhenPollingStartedRef = useRef<number | null>(null);
  const refetchMiners = onRefetchMiners;

  // Track latest poolNeededCount to avoid stale closure in callbacks
  const poolNeededCountRef = useRef(poolNeededCount);
  useEffect(() => {
    poolNeededCountRef.current = poolNeededCount;
  }, [poolNeededCount]);

  // Polls for status updates with fixed 2s intervals after 1s initial delay.
  // Returns cleanup function to cancel pending polls.
  const pollForStatusUpdates = useCallback(() => {
    setIsRefreshingEquipment(true);
    poolCountWhenPollingStartedRef.current = poolNeededCountRef.current;

    let pollCount = 0;
    const maxPolls = 10;
    const pollIntervalMs = 2000;
    const initialDelayMs = 1000;
    const timeouts: ReturnType<typeof setTimeout>[] = [];
    let cancelled = false;

    const resetPollingState = () => {
      pollingCleanupRef.current = null;
      poolCountWhenPollingStartedRef.current = null;
      setIsRefreshingEquipment(false);
    };

    const poll = () => {
      if (cancelled) return;

      refetchAuthNeededMiners();
      refetchPoolNeededCount();
      pollCount += 1;

      if (pollCount < maxPolls) {
        timeouts.push(setTimeout(poll, pollIntervalMs));
      } else {
        // Max polls reached - finalize and reset
        resetPollingState();
      }
    };

    // Initial delay gives backend time to process updates
    timeouts.push(setTimeout(poll, initialDelayMs));

    const cleanup = () => {
      cancelled = true;
      timeouts.forEach(clearTimeout);
      resetPollingState();
    };

    pollingCleanupRef.current = cleanup;
    return cleanup;
  }, [refetchAuthNeededMiners, refetchPoolNeededCount]);

  useEffect(() => {
    if (
      pollingCleanupRef.current &&
      poolCountWhenPollingStartedRef.current !== null &&
      poolCountWhenPollingStartedRef.current !== poolNeededCount
    )
      pollingCleanupRef.current();
  }, [poolNeededCount]);

  // Ensure polling is cleaned up if the component unmounts while polling is active
  useEffect(() => {
    return () => {
      pollingCleanupRef.current?.();
    };
  }, []);

  const handlePoolDismiss = useCallback(() => {
    setShowPoolSelectionModal(false);
    refetchMiners?.();
    refetchPoolNeededCount();
  }, [refetchMiners, refetchPoolNeededCount]);

  // Watch for pairing operations completing and start polling
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
      {shouldShow ? (
        <div className={className} data-testid="complete-setup">
          <div className="@container rounded-xl bg-surface-elevated-base p-6 shadow-100">
            <div className="mb-6 flex items-center justify-between gap-x-10">
              <div className="text-heading-300">Complete setup</div>
              <Button
                ariaLabel="Dismiss complete setup"
                onClick={handleDismiss}
                variant="secondary"
                prefixIcon={<Dismiss />}
              />
            </div>
            <div className="grid gap-4 @lg:grid-cols-2 @3xl:grid-cols-3 @7xl:grid-cols-4">
              <AnimatePresence mode="popLayout">
                {hasConfigurePoolCard ? (
                  <motion.div
                    key="configure-pool-card"
                    layout
                    initial={{ opacity: 0, scale: 0.95 }}
                    animate={{ opacity: 1, scale: 1 }}
                    exit={{ opacity: 0, scale: 0.95 }}
                    transition={{ duration: 0.2, ease: "easeOut" }}
                  >
                    <ConfigurePoolCard
                      count={poolNeededCount}
                      onConfigureClick={() => {
                        if (poolNeededCount === 0) {
                          return;
                        }

                        setShowPoolSelectionModal(true);
                      }}
                      isLoading={isLoadingPoolNeeded || isRefreshingEquipment}
                    />
                  </motion.div>
                ) : null}
                {hasAuthCard ? (
                  <motion.div
                    key="auth-card"
                    layout
                    initial={{ opacity: 0, scale: 0.95 }}
                    animate={{ opacity: 1, scale: 1 }}
                    exit={{ opacity: 0, scale: 0.95 }}
                    transition={{ duration: 0.2, ease: "easeOut" }}
                  >
                    <AuthenticateMinersCard
                      count={authNeededCount}
                      onAuthenticationSuccess={refetchAuthNeededMiners}
                      onRefetchMiners={refetchMiners}
                      onPairingCompleted={onPairingCompleted}
                    />
                  </motion.div>
                ) : null}
              </AnimatePresence>
            </div>
          </div>
        </div>
      ) : null}
      <PoolSelectionPageWrapper
        open={showPoolSelectionModal}
        selectionMode="all"
        poolNeededCount={poolNeededCount}
        filterCriteria={{
          deviceStatus: DeviceStatus.NEEDS_MINING_POOL,
          pairingStatus: PairingStatus.PAIRED,
        }}
        onApplied={() => {
          pollForStatusUpdates();
        }}
        onDismiss={handlePoolDismiss}
      />
    </>
  );
};

export default CompleteSetup;
