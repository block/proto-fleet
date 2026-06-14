import { useCallback, useEffect, useState } from "react";
import type { ReactElement } from "react";
import clsx from "clsx";

import type { ActiveRolloutAction } from "./ActiveFirmwareRollout";
import {
  type FirmwareRollout,
  type FirmwareRolloutEvent,
  FirmwareRolloutState,
  type FirmwareRolloutTarget,
  FirmwareRolloutTargetState,
} from "@/protoFleet/api/generated/firmwarerollout/v1/firmwarerollout_pb";
import { useFirmwareRolloutApi } from "@/protoFleet/api/useFirmwareRolloutApi";
import FirmwareRolloutStatusPill from "@/protoFleet/features/firmwareRollouts/components/FirmwareRolloutStatusPill";
import {
  firmwareRolloutTargetStateConfig,
  formatRolloutTimestamp,
} from "@/protoFleet/features/firmwareRollouts/firmwareRolloutDisplayUtils";
import Button, { sizes, variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";

const targetsPageSize = 100;

interface FirmwareRolloutDetailModalProps {
  rollout: FirmwareRollout;
  open: boolean;
  onDismiss: () => void;
  onAction: (rollout: FirmwareRollout, action: ActiveRolloutAction) => void;
  actionPending?: boolean;
}

function StatTile({ label, value }: { label: string; value: number }): ReactElement {
  return (
    <div className="rounded-lg bg-surface-5 p-3">
      <div className="text-100 text-text-primary-50">{label}</div>
      <div className="text-heading-100 text-text-primary">{value.toLocaleString()}</div>
    </div>
  );
}

const FirmwareRolloutDetailModal = ({
  rollout,
  open,
  onDismiss,
  onAction,
  actionPending = false,
}: FirmwareRolloutDetailModalProps): ReactElement => {
  const rolloutApi = useFirmwareRolloutApi();
  const [targets, setTargets] = useState<FirmwareRolloutTarget[]>([]);
  const [events, setEvents] = useState<FirmwareRolloutEvent[]>([]);
  const [pageToken, setPageToken] = useState("");
  const [hasMore, setHasMore] = useState(false);
  const [failedOnly, setFailedOnly] = useState(false);
  const [isLoading, setIsLoading] = useState(true);

  const counts = rollout.counts;
  const countsSignature = counts
    ? `${counts.successCount}-${counts.failureCount}-${counts.pendingCount}-${counts.inProgressCount}-${counts.canceledCount}`
    : "";

  // Load (and live-refresh) results whenever the modal opens, the failed-only
  // filter changes, or the rollout's counts change while it is open. The spinner
  // only shows on first mount (isLoading starts true); poll refreshes update
  // silently. The modal is keyed by rolloutId by the parent, so it mounts fresh.
  useEffect(() => {
    if (!open) return;
    let canceled = false;
    rolloutApi
      .listTargets({
        rolloutId: rollout.rolloutId,
        pageSize: targetsPageSize,
        pageToken: "",
        stateFilter: failedOnly ? FirmwareRolloutTargetState.FAILED : undefined,
      })
      .then((targetPage) => {
        if (canceled) return;
        setTargets(targetPage.targets);
        setPageToken(targetPage.nextPageToken);
        setHasMore(targetPage.nextPageToken !== "");
      })
      .catch(() => undefined)
      .finally(() => {
        if (!canceled) setIsLoading(false);
      });
    rolloutApi
      .listEvents(rollout.rolloutId)
      .then((timeline) => {
        if (!canceled) setEvents(timeline);
      })
      .catch(() => undefined);
    return () => {
      canceled = true;
    };
  }, [open, rollout.rolloutId, countsSignature, failedOnly, rolloutApi]);

  const loadMore = useCallback(async () => {
    const targetPage = await rolloutApi.listTargets({
      rolloutId: rollout.rolloutId,
      pageSize: targetsPageSize,
      pageToken,
      stateFilter: failedOnly ? FirmwareRolloutTargetState.FAILED : undefined,
    });
    setTargets((prev) => [...prev, ...targetPage.targets]);
    setPageToken(targetPage.nextPageToken);
    setHasMore(targetPage.nextPageToken !== "");
  }, [failedOnly, pageToken, rollout.rolloutId, rolloutApi]);

  const handleFailedOnlyChange = (value: boolean) => setFailedOnly(value);

  const isPaused = rollout.state === FirmwareRolloutState.PAUSED;
  const isRunning = rollout.state === FirmwareRolloutState.RUNNING;
  const canRetry =
    (counts?.failureCount ?? 0) > 0 && (isPaused || rollout.state === FirmwareRolloutState.COMPLETED_WITH_FAILURES);
  const isActive = isRunning || isPaused || rollout.state === FirmwareRolloutState.DRAFT;

  return (
    <Modal open={open} onDismiss={onDismiss} title={rollout.name} size="large">
      <div className="flex flex-col gap-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <FirmwareRolloutStatusPill state={rollout.state} />
            <span className="text-200 text-text-primary-50">
              {rollout.minerModel || "—"} · firmware {rollout.firmwareFileId} · batch {rollout.batchSize} every{" "}
              {rollout.batchIntervalSeconds}s
            </span>
          </div>
          <div className="flex flex-wrap gap-2">
            {isRunning ? (
              <Button
                variant={variants.secondary}
                size={sizes.compact}
                text="Pause"
                disabled={actionPending}
                onClick={() => onAction(rollout, "pause")}
              />
            ) : null}
            {isPaused ? (
              <Button
                variant={variants.secondary}
                size={sizes.compact}
                text="Resume"
                disabled={actionPending}
                onClick={() => onAction(rollout, "resume")}
              />
            ) : null}
            {canRetry ? (
              <Button
                variant={variants.primary}
                size={sizes.compact}
                text="Retry failed miners"
                disabled={actionPending}
                onClick={() => onAction(rollout, "retry")}
              />
            ) : null}
            {isActive ? (
              <Button
                variant={variants.secondaryDanger}
                size={sizes.compact}
                text="Abort"
                disabled={actionPending}
                onClick={() => onAction(rollout, "abort")}
              />
            ) : null}
          </div>
        </div>

        <div className="grid grid-cols-2 gap-3 laptop:grid-cols-4">
          <StatTile label="Targeted" value={counts?.totalCount ?? rollout.targetCount} />
          <StatTile label="Succeeded" value={counts?.successCount ?? 0} />
          <StatTile label="Failed" value={counts?.failureCount ?? 0} />
          <StatTile label="In progress" value={counts?.inProgressCount ?? 0} />
          <StatTile label="Pending" value={counts?.pendingCount ?? 0} />
          <StatTile label="Aborted" value={counts?.canceledCount ?? 0} />
          <StatTile label="Retried" value={counts?.retriedCount ?? 0} />
        </div>

        <div>
          <div className="mb-3 flex items-center justify-between">
            <div className="text-300 font-semibold text-text-primary">Miner results</div>
            <label className="flex items-center gap-2 text-200 text-text-primary">
              <input type="checkbox" checked={failedOnly} onChange={(e) => handleFailedOnlyChange(e.target.checked)} />
              Failed only
            </label>
          </div>
          {isLoading ? (
            <div className="flex justify-center py-10">
              <ProgressCircular indeterminate />
            </div>
          ) : (
            <div className="max-h-80 overflow-y-auto rounded-lg border border-border-5">
              <table className="w-full text-200">
                <thead className="sticky top-0 bg-surface-5 text-left text-text-primary-50">
                  <tr>
                    <th className="px-3 py-2">Miner</th>
                    <th className="px-3 py-2">Status</th>
                    <th className="px-3 py-2">Attempt</th>
                    <th className="px-3 py-2">Message</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border-5">
                  {targets.length === 0 ? (
                    <tr>
                      <td className="px-3 py-4 text-text-primary-50" colSpan={4}>
                        No miner results yet.
                      </td>
                    </tr>
                  ) : (
                    targets.map((target) => {
                      const config = firmwareRolloutTargetStateConfig(target.state);
                      return (
                        <tr key={target.deviceIdentifier}>
                          <td className="px-3 py-2">
                            <div className="text-text-primary">{target.deviceName || target.deviceIdentifier}</div>
                            <div className="text-100 font-mono text-text-primary-50">
                              {[target.macAddress, target.ipAddress].filter(Boolean).join(" · ")}
                            </div>
                          </td>
                          <td className="px-3 py-2">
                            <span className="inline-flex items-center gap-2 text-text-primary">
                              <span
                                className={clsx("inline-block h-2 w-2 shrink-0 rounded-full", config.dotClassName)}
                              />
                              {config.label}
                            </span>
                          </td>
                          <td className="px-3 py-2 text-text-primary">{target.currentAttemptNumber || "—"}</td>
                          <td className="max-w-sm truncate px-3 py-2 text-text-primary-50">
                            {target.lastError || "—"}
                          </td>
                        </tr>
                      );
                    })
                  )}
                </tbody>
              </table>
            </div>
          )}
          {hasMore ? (
            <div className="mt-3 flex justify-center">
              <Button
                variant={variants.secondary}
                size={sizes.compact}
                text="Load more miners"
                onClick={() => void loadMore()}
              />
            </div>
          ) : null}
        </div>

        <div>
          <div className="mb-3 text-300 font-semibold text-text-primary">Timeline</div>
          <div className="flex flex-col gap-2">
            {events.length === 0 ? (
              <div className="text-200 text-text-primary-50">No rollout events yet.</div>
            ) : (
              events.map((event) => (
                <div key={`${event.eventType}-${event.createdAt?.seconds}`} className="rounded-lg bg-surface-5 p-3">
                  <div className="text-300 text-text-primary">{event.message}</div>
                  <div className="text-200 text-text-primary-50">
                    {event.username || event.actorType} · {formatRolloutTimestamp(event.createdAt?.seconds)}
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      </div>
    </Modal>
  );
};

export default FirmwareRolloutDetailModal;
