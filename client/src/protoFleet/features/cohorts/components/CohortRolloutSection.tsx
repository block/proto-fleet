import { useEffect, useMemo, useState } from "react";

import {
  type Cohort,
  CohortConfigDimension,
  CohortState,
  type GetCohortFirmwareVersionHistoryResponse,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import { useCohortApi } from "@/protoFleet/api/useCohortApi";
import FirmwareValidationSection from "@/protoFleet/features/cohorts/components/FirmwareValidationSection";
import FirmwareVersionHistoryPanel from "@/protoFleet/features/cohorts/components/FirmwareVersionHistoryPanel";
import SectionHeading from "@/protoFleet/features/dashboard/components/SectionHeading";
import { getGranularityForDuration } from "@/protoFleet/features/dashboard/utils/granularity";
import { useDuration, useSetDuration } from "@/protoFleet/store";
import { Plus } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import DurationSelector, { fleetDurations, getFleetDurationMs } from "@/shared/components/DurationSelector";

type RolloutSegment = {
  label: string;
  count: number;
  colorClass: string;
};

interface RolloutProgressCardProps {
  title: string;
  subtitle: string;
  targetedCount: number;
  enforced: boolean;
  segments: RolloutSegment[];
}

const RolloutProgressCard = ({ title, subtitle, targetedCount, enforced, segments }: RolloutProgressCardProps) => {
  const segmentTotal = segments.reduce((total, segment) => total + segment.count, 0);
  const denominator = Math.max(targetedCount, segmentTotal);

  return (
    <div
      className="rounded-xl border border-border-5 bg-surface-base p-5"
      data-testid={`rollout-${title.toLowerCase()}`}
    >
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <h3 className="text-heading-100 text-text-primary">{title}</h3>
          <p className="mt-1 text-200 text-text-primary-70">{subtitle}</p>
        </div>
        <span className="rounded-full bg-core-primary-5 px-3 py-1 text-emphasis-200 text-text-primary">
          {enforced ? `${targetedCount} targeted` : "Not enforced"}
        </span>
      </div>

      {enforced ? (
        <>
          <div
            className="mt-5 flex h-3 w-full overflow-hidden rounded-full bg-core-primary-5"
            role="img"
            aria-label={`${title} rollout: ${segments.map((segment) => `${segment.label} ${segment.count}`).join(", ")}`}
          >
            {denominator > 0
              ? segments.map((segment) =>
                  segment.count > 0 ? (
                    <div
                      key={segment.label}
                      className={segment.colorClass}
                      style={{ width: `${(segment.count / denominator) * 100}%` }}
                      title={`${segment.label}: ${segment.count}`}
                    />
                  ) : null,
                )
              : null}
          </div>
          <div className="mt-4 grid gap-x-5 gap-y-2 tablet:grid-cols-2">
            {segments.map((segment) => (
              <div key={segment.label} className="flex items-center justify-between gap-3 text-200">
                <span className="flex min-w-0 items-center gap-2 text-text-primary-70">
                  <span className={`h-2.5 w-2.5 shrink-0 rounded-full ${segment.colorClass}`} />
                  <span className="truncate">{segment.label}</span>
                </span>
                <span className="font-medium text-text-primary">{segment.count}</span>
              </div>
            ))}
          </div>
        </>
      ) : (
        <div className="mt-5 rounded-lg bg-core-primary-5 px-4 py-5 text-300 text-text-primary-70">
          Set a {title.toLowerCase()} target to track convergence.
        </div>
      )}
    </div>
  );
};

interface CohortRolloutSectionProps {
  cohort: Cohort;
  firmwareTargetLabel: string;
  canAddMiners: boolean;
  onAddMiners: () => void;
}

const CohortRolloutSection = ({
  cohort,
  firmwareTargetLabel,
  canAddMiners,
  onAddMiners,
}: CohortRolloutSectionProps) => {
  const summary = cohort.summary;
  const duration = useDuration();
  const setDuration = useSetDuration();
  const { getFirmwareVersionHistory } = useCohortApi();
  const [firmwareHistory, setFirmwareHistory] = useState<GetCohortFirmwareVersionHistoryResponse | null>(null);
  const [firmwareHistoryLoading, setFirmwareHistoryLoading] = useState(false);
  const [firmwareHistoryError, setFirmwareHistoryError] = useState(false);
  const cohortId = summary?.id;
  const deviceIds = useMemo(() => cohort.members.map((member) => member.deviceIdentifier), [cohort.members]);
  const hasMiners = deviceIds.length > 0;
  const firmwareHistoryRefreshKey = useMemo(
    () =>
      cohort.members
        .map(
          (member) =>
            `${member.deviceIdentifier}:${member.firmwareStatus?.currentFirmwareVersion || member.display?.firmwareVersion || ""}`,
        )
        .sort()
        .join("|"),
    [cohort.members],
  );

  useEffect(() => {
    if (cohortId === undefined || !hasMiners) return undefined;

    let cancelled = false;
    const load = async () => {
      setFirmwareHistory(null);
      setFirmwareHistoryLoading(true);
      setFirmwareHistoryError(false);
      const endTime = new Date();
      const startTime = new Date(endTime.getTime() - getFleetDurationMs(duration));
      try {
        const history = await getFirmwareVersionHistory({
          cohortId,
          startTime,
          endTime,
          granularitySeconds: getGranularityForDuration(duration),
        });
        if (!cancelled) setFirmwareHistory(history);
      } catch {
        if (!cancelled) setFirmwareHistoryError(true);
      } finally {
        if (!cancelled) setFirmwareHistoryLoading(false);
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [cohortId, duration, firmwareHistoryRefreshKey, getFirmwareVersionHistory, hasMiners]);

  if (!summary) return null;

  if (!hasMiners) {
    const isReleased = summary.state === CohortState.RELEASED;
    return (
      <section
        className="rounded-xl border border-border-5 bg-surface-base px-6 py-10 text-center"
        data-testid="cohort-empty-rollout-state"
      >
        <h2 className="text-heading-200 text-text-primary">
          {isReleased ? "No miners remain in this cohort" : "Add miners to begin cohort validation"}
        </h2>
        <p className="mx-auto mt-2 max-w-xl text-300 text-text-primary-70">
          {isReleased
            ? "Historical rollout and validation graphs are unavailable because this released cohort is empty."
            : "Once miners are reserved, this page will track desired-state convergence, firmware rollout history, and validation outcomes."}
        </p>
        {!isReleased && canAddMiners ? (
          <Button
            className="mx-auto mt-5"
            text="Add miners"
            prefixIcon={<Plus />}
            size={sizes.compact}
            variant={variants.primary}
            onClick={onAddMiners}
          />
        ) : null}
      </section>
    );
  }

  const firmwareProgress = summary.firmwareProgress;
  const poolProgress = summary.configProgress.find((progress) => progress.dimension === CohortConfigDimension.POOLS);
  const firmwareEnforced = Boolean(
    summary.desiredFirmwareFileId ||
    cohort.firmwareTargets.some((target) => target.firmwareFileId) ||
    summary.firmwareTargets.some((target) => target.firmwareFileId),
  );
  const poolsEnforced = Boolean(summary.desiredConfig?.pools);
  const firmwareSegments: RolloutSegment[] = [
    { label: "Complete", count: firmwareProgress?.completeCount ?? 0, colorClass: "bg-intent-success-fill" },
    {
      label: "In progress",
      count:
        (firmwareProgress?.queuedCount ?? 0) +
        (firmwareProgress?.updatingCount ?? 0) +
        (firmwareProgress?.verifyingCount ?? 0),
      colorClass: "bg-core-accent-fill",
    },
    {
      label: "Needs attention",
      count: firmwareProgress?.needsAttentionCount ?? 0,
      colorClass: "bg-intent-critical-fill",
    },
    { label: "Unknown", count: firmwareProgress?.unknownCount ?? 0, colorClass: "bg-core-primary-20" },
  ];
  const poolSegments: RolloutSegment[] = [
    { label: "Converged", count: poolProgress?.convergedCount ?? 0, colorClass: "bg-intent-success-fill" },
    {
      label: "In progress",
      count:
        (poolProgress?.waitingCount ?? 0) + (poolProgress?.applyingCount ?? 0) + (poolProgress?.verifyingCount ?? 0),
      colorClass: "bg-core-accent-fill",
    },
    { label: "Held", count: poolProgress?.heldCount ?? 0, colorClass: "bg-intent-warning-fill" },
    { label: "Failed", count: poolProgress?.failedCount ?? 0, colorClass: "bg-intent-critical-fill" },
    { label: "Unsupported", count: poolProgress?.unsupportedCount ?? 0, colorClass: "bg-core-primary-20" },
  ];
  const historyHeadline = firmwareEnforced
    ? `Target ${firmwareTargetLabel} · ${firmwareProgress?.completeCount ?? 0}/${firmwareProgress?.targetedCount ?? 0} complete`
    : "No firmware target";

  return (
    <div className="flex flex-col gap-8" data-testid="cohort-rollout-section">
      <section>
        <SectionHeading heading="Rollout status" />
        <div className="mt-4 grid gap-4 desktop:grid-cols-2">
          <RolloutProgressCard
            title="Firmware"
            subtitle="Current fleet progress toward the selected firmware version."
            targetedCount={firmwareProgress?.targetedCount ?? 0}
            enforced={firmwareEnforced}
            segments={firmwareSegments}
          />
          <RolloutProgressCard
            title="Pools"
            subtitle="Current fleet progress toward the desired pool configuration."
            targetedCount={poolProgress?.targetedCount ?? 0}
            enforced={poolsEnforced}
            segments={poolSegments}
          />
        </div>
      </section>

      <section>
        <SectionHeading heading="Firmware rollout history">
          <DurationSelector duration={duration} durations={fleetDurations} onSelect={setDuration} />
        </SectionHeading>
        <div className="mt-4">
          <FirmwareVersionHistoryPanel
            title="Version mix"
            headline={historyHeadline}
            duration={duration}
            history={firmwareHistory}
            isLoading={firmwareHistoryLoading}
            hasError={firmwareHistoryError}
          />
        </div>
      </section>

      <FirmwareValidationSection cohort={cohort} />
    </div>
  );
};

export default CohortRolloutSection;
