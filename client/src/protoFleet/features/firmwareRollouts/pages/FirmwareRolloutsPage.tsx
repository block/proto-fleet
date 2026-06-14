import { useCallback, useEffect, useMemo, useState } from "react";

import { type FirmwareRollout } from "@/protoFleet/api/generated/firmwarerollout/v1/firmwarerollout_pb";
import { useFirmwareRolloutApi } from "@/protoFleet/api/useFirmwareRolloutApi";
import ActiveFirmwareRollout, {
  type ActiveRolloutAction,
} from "@/protoFleet/features/firmwareRollouts/components/ActiveFirmwareRollout";
import CreateFirmwareRolloutModal from "@/protoFleet/features/firmwareRollouts/components/CreateFirmwareRolloutModal";
import FirmwareRolloutDetailModal from "@/protoFleet/features/firmwareRollouts/components/FirmwareRolloutDetailModal";
import FirmwareRolloutHistory from "@/protoFleet/features/firmwareRollouts/components/FirmwareRolloutHistory";
import { isActiveRolloutState } from "@/protoFleet/features/firmwareRollouts/firmwareRolloutDisplayUtils";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import DialogIcon from "@/shared/components/Dialog/DialogIcon";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";

const rolloutsPageSize = 100;
const pollingMs = 5000;

// Transport-level failures (dev proxy can't reach the backend, or the backend is
// restarting and returns a 5xx) surface as "Failed to fetch" / "HTTP 50x". Show a
// calmer message for those; they clear on the next successful refresh.
function describeError(err: unknown, fallback: string): string {
  const message = err instanceof Error ? err.message : "";
  if (!message) return fallback;
  if (message === "Failed to fetch" || /^HTTP 5\d\d/.test(message)) {
    return "Couldn’t reach the server — it may be restarting. This clears automatically once it’s reachable.";
  }
  return message;
}

const FirmwareRolloutsPage = () => {
  const rolloutApi = useFirmwareRolloutApi();

  const [rollouts, setRollouts] = useState<FirmwareRollout[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [selectedRolloutId, setSelectedRolloutId] = useState<string | null>(null);
  const [abortTarget, setAbortTarget] = useState<FirmwareRollout | null>(null);
  const [actionPending, setActionPending] = useState(false);

  // Refresh the list and clear any stale error on success, so a transient blip
  // (e.g. the backend restarting) recovers on its own.
  const loadRollouts = useCallback(async () => {
    const response = await rolloutApi.listRollouts("", rolloutsPageSize);
    setRollouts(response.rollouts);
    setError(null);
  }, [rolloutApi]);

  useEffect(() => {
    let canceled = false;
    rolloutApi
      .listRollouts("", rolloutsPageSize)
      .then((response) => {
        if (canceled) return;
        setRollouts(response.rollouts);
        setError(null);
      })
      .catch((err) => {
        if (!canceled) setError(describeError(err, "Failed to load firmware rollouts"));
      })
      .finally(() => {
        if (!canceled) setIsLoading(false);
      });
    return () => {
      canceled = true;
    };
  }, [rolloutApi]);

  const handleRetry = useCallback(() => {
    setError(null);
    void loadRollouts().catch((err) => setError(describeError(err, "Failed to load firmware rollouts")));
  }, [loadRollouts]);

  const activeRollouts = useMemo(() => rollouts.filter((r) => isActiveRolloutState(r.state)), [rollouts]);
  const historyRollouts = useMemo(() => rollouts.filter((r) => !isActiveRolloutState(r.state)), [rollouts]);
  const activeModels = useMemo(() => new Set(activeRollouts.map((r) => r.minerModel)), [activeRollouts]);
  const selectedRollout = selectedRolloutId ? (rollouts.find((r) => r.rolloutId === selectedRolloutId) ?? null) : null;

  // Poll while any rollout is still dispatching. Background refreshes swallow
  // transient failures (they clear the banner on the next success).
  useEffect(() => {
    if (activeRollouts.length === 0) return;
    const interval = setInterval(() => {
      void loadRollouts().catch(() => undefined);
    }, pollingMs);
    return () => clearInterval(interval);
  }, [activeRollouts.length, loadRollouts]);

  const runAction = useCallback(
    async (rollout: FirmwareRollout, action: Exclude<ActiveRolloutAction, "abort">) => {
      setActionPending(true);
      setError(null);
      try {
        if (action === "pause") await rolloutApi.pauseRollout(rollout.rolloutId);
        else if (action === "resume") await rolloutApi.resumeRollout(rollout.rolloutId);
        else await rolloutApi.retryFailedTargets(rollout.rolloutId);
        await loadRollouts();
      } catch (err) {
        setError(describeError(err, "Rollout action failed"));
      } finally {
        setActionPending(false);
      }
    },
    [loadRollouts, rolloutApi],
  );

  const handleAction = useCallback(
    (rollout: FirmwareRollout, action: ActiveRolloutAction) => {
      if (action === "abort") {
        setAbortTarget(rollout);
        return;
      }
      void runAction(rollout, action);
    },
    [runAction],
  );

  const confirmAbort = useCallback(async () => {
    if (!abortTarget) return;
    setActionPending(true);
    setError(null);
    try {
      await rolloutApi.cancelRollout(abortTarget.rolloutId);
      setAbortTarget(null);
      await loadRollouts();
    } catch (err) {
      setError(describeError(err, "Failed to abort rollout"));
    } finally {
      setActionPending(false);
    }
  }, [abortTarget, loadRollouts, rolloutApi]);

  const handleCreated = useCallback(
    (rollout: FirmwareRollout) => {
      setShowCreateModal(false);
      setRollouts((prev) => [rollout, ...prev.filter((r) => r.rolloutId !== rollout.rolloutId)]);
      void loadRollouts();
    },
    [loadRollouts],
  );

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  return (
    <div className="p-6 laptop:p-10">
      <section className="grid gap-6">
        <div className="flex items-center justify-between gap-4 phone:flex-col phone:items-stretch">
          <Header title="Firmware Rollouts" titleSize="text-heading-300" />
          <Button variant={variants.primary} text="New rollout" onClick={() => setShowCreateModal(true)} />
        </div>

        {error ? (
          <div className="flex items-center justify-between gap-3 rounded-lg bg-intent-warning-10 px-4 py-3 text-300 text-text-primary">
            <div className="flex items-center gap-3">
              <Alert className="shrink-0 text-intent-warning-fill" />
              <span className="text-emphasis-300">{error}</span>
            </div>
            <Button variant={variants.secondary} size={sizes.compact} text="Retry" onClick={handleRetry} />
          </div>
        ) : null}

        {activeRollouts.length > 0 ? (
          <section className="grid gap-3">
            <Header title="Active rollouts" titleSize="text-heading-200" />
            <div className="grid gap-4">
              {activeRollouts.map((rollout) => (
                <ActiveFirmwareRollout
                  key={rollout.rolloutId}
                  rollout={rollout}
                  pending={actionPending}
                  onAction={handleAction}
                  onViewDetails={(r) => setSelectedRolloutId(r.rolloutId)}
                />
              ))}
            </div>
          </section>
        ) : (
          <div className="rounded-xl border border-border-5 bg-surface-base p-6 text-300 text-text-primary-50">
            No active rollouts. Start one with “New rollout”.
          </div>
        )}

        <FirmwareRolloutHistory rollouts={historyRollouts} onSelect={(r) => setSelectedRolloutId(r.rolloutId)} />
      </section>

      <CreateFirmwareRolloutModal
        open={showCreateModal}
        activeModels={activeModels}
        onDismiss={() => setShowCreateModal(false)}
        onCreated={handleCreated}
      />

      {selectedRollout ? (
        <FirmwareRolloutDetailModal
          key={selectedRollout.rolloutId}
          rollout={selectedRollout}
          open
          onDismiss={() => setSelectedRolloutId(null)}
          onAction={handleAction}
          actionPending={actionPending}
        />
      ) : null}

      {abortTarget ? (
        <Dialog
          open
          title="Abort firmware rollout?"
          icon={
            <DialogIcon intent="critical">
              <Alert />
            </DialogIcon>
          }
          buttons={[
            {
              text: "Keep running",
              variant: variants.secondary,
              onClick: () => setAbortTarget(null),
            },
            {
              text: "Abort rollout",
              variant: variants.danger,
              loading: actionPending,
              onClick: () => void confirmAbort(),
            },
          ]}
          onDismiss={() => setAbortTarget(null)}
        >
          <div className="text-300 text-text-primary-70">
            Aborting stops dispatching new batches for <span className="font-semibold">{abortTarget.name}</span> (
            {abortTarget.minerModel || "—"}). Miners already updated stay on the new firmware; miners not yet reached
            keep their current firmware. This frees the model so you can start a new rollout.
          </div>
        </Dialog>
      ) : null}
    </div>
  );
};

export default FirmwareRolloutsPage;
