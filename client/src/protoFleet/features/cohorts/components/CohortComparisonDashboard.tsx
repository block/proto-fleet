import { useMemo } from "react";

import {
  CohortConfigDimension,
  type CohortSummary,
  type CohortTelemetryComparisonDistribution,
  CohortTelemetryComparisonMetric,
  type CohortTelemetryComparisonSeries,
  CohortTelemetryComparisonWindow,
  type GetCohortTelemetryComparisonResponse,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import {
  type ConvergenceSegment,
  firmwareConvergenceSegments,
  poolConvergenceSegments,
} from "@/protoFleet/features/cohorts/components/cohortComparison";
import SectionHeading from "@/protoFleet/features/dashboard/components/SectionHeading";
import {
  normalizeEfficiencyToJTH,
  normalizeHashrateToTHs,
  normalizePowerToKW,
} from "@/protoFleet/features/dashboard/utils/metricNormalization";
import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import Checkbox from "@/shared/components/Checkbox";
import SkeletonBar from "@/shared/components/SkeletonBar";

const cohortColors = [
  "var(--color-extended-navy-fill)",
  "var(--color-extended-teal-fill)",
  "var(--color-extended-forest-fill)",
  "var(--color-extended-purple-fill)",
  "var(--color-extended-pink-fill)",
  "var(--color-extended-sky-fill)",
];
const defaultCohortColor = "var(--color-core-primary-50)";

const comparisonWindowOptions = [
  { value: CohortTelemetryComparisonWindow.ONE_HOUR, label: "1h" },
  { value: CohortTelemetryComparisonWindow.SIX_HOURS, label: "6h" },
  { value: CohortTelemetryComparisonWindow.TWENTY_FOUR_HOURS, label: "24h" },
];

const cohortColor = (cohort: Pick<CohortSummary, "id" | "isDefault">) => {
  if (cohort.isDefault) return defaultCohortColor;
  const paletteIndex = Number(cohort.id % BigInt(cohortColors.length));
  return cohortColors[paletteIndex];
};

const FleetAllocation = ({ cohorts }: { cohorts: CohortSummary[] }) => {
  const totalMiners = cohorts.reduce((total, cohort) => total + Number(cohort.explicitMemberCount), 0);

  return (
    <section className="rounded-xl border border-border-5 bg-surface-base p-5" data-testid="cohort-allocation">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-heading-200 text-text-primary">Fleet allocation</h2>
          <p className="mt-1 text-300 text-text-primary-70">
            Current miner distribution across the default cohort and rollout cohorts.
          </p>
        </div>
        <span className="rounded-full bg-core-primary-5 px-3 py-1 text-emphasis-200 text-text-primary">
          {totalMiners.toLocaleString()} miners
        </span>
      </div>

      {totalMiners > 0 ? (
        <>
          <div
            className="mt-6 flex h-8 w-full overflow-hidden rounded-lg bg-core-primary-5"
            role="img"
            aria-label={cohorts
              .map((cohort) => `${cohort.label}: ${cohort.explicitMemberCount.toString()} miners`)
              .join(", ")}
          >
            {cohorts.map((cohort) => {
              const count = Number(cohort.explicitMemberCount);
              if (count === 0) return null;
              const percentage = (count / totalMiners) * 100;
              return (
                <div
                  key={cohort.id.toString()}
                  data-cohort-id={cohort.id.toString()}
                  className="text-emphasis-100 flex items-center justify-center overflow-hidden px-1 text-white"
                  style={{ width: `${percentage}%`, backgroundColor: cohortColor(cohort) }}
                  title={`${cohort.label}: ${count.toLocaleString()}`}
                >
                  {percentage >= 8 ? count.toLocaleString() : null}
                </div>
              );
            })}
          </div>
          <div className="mt-5 flex flex-wrap gap-x-6 gap-y-3">
            {cohorts.map((cohort) => {
              const count = Number(cohort.explicitMemberCount);
              return (
                <div key={cohort.id.toString()} className="flex min-w-0 items-center gap-2 text-300">
                  <span
                    className="h-2.5 w-2.5 shrink-0 rounded-full"
                    style={{ backgroundColor: cohortColor(cohort) }}
                  />
                  <span className="max-w-48 truncate text-text-primary-70">{cohort.label}</span>
                  <span className="shrink-0 font-medium text-text-primary">
                    {count.toLocaleString()} · {Math.round((count / totalMiners) * 100)}%
                  </span>
                </div>
              );
            })}
          </div>
        </>
      ) : (
        <div className="mt-6 rounded-lg bg-core-primary-5 px-4 py-8 text-center text-300 text-text-primary-70">
          Add miners to start comparing fleet allocation.
        </div>
      )}
    </section>
  );
};

const CohortSelector = ({
  cohorts,
  selectedIds,
  onToggle,
}: {
  cohorts: CohortSummary[];
  selectedIds: string[];
  onToggle: (cohortId: string) => void;
}) => (
  <section className="rounded-xl border border-border-5 bg-surface-base p-5" data-testid="cohort-comparison-selector">
    <div className="flex flex-wrap items-start justify-between gap-2">
      <div>
        <h2 className="text-heading-100 text-text-primary">Compare cohorts</h2>
        <p className="mt-1 text-200 text-text-primary-70">Choose up to five. The default cohort stays pinned.</p>
      </div>
      <span className="text-emphasis-200 text-text-primary-70">{selectedIds.length}/5 selected</span>
    </div>
    <div className="mt-4 grid max-h-48 gap-2 overflow-y-auto pr-1 tablet:grid-cols-2 desktop:grid-cols-3">
      {cohorts.map((cohort) => {
        const id = cohort.id.toString();
        const selected = selectedIds.includes(id);
        const disabled = cohort.isDefault || (!selected && selectedIds.length >= 5);
        return (
          <label
            key={id}
            className="flex cursor-pointer items-center gap-3 rounded-lg border border-border-5 px-3 py-2 text-300"
          >
            <Checkbox checked={selected} disabled={disabled} onChange={() => onToggle(id)} />
            <span className="min-w-0 grow truncate text-text-primary">{cohort.label}</span>
            <span className="shrink-0 text-200 text-text-primary-50">{cohort.explicitMemberCount.toString()}</span>
          </label>
        );
      })}
    </div>
  </section>
);

const ConvergenceBar = ({
  cohort,
  segments,
  enforced,
  targetedCount,
}: {
  cohort: CohortSummary;
  segments: ConvergenceSegment[];
  enforced: boolean;
  targetedCount: number;
}) => {
  const segmentTotal = segments.reduce((total, segment) => total + segment.count, 0);
  const denominator = Math.max(targetedCount, segmentTotal, 1);
  return (
    <div className="grid items-center gap-2 tablet:grid-cols-[minmax(8rem,0.8fr)_minmax(12rem,2fr)_5rem]">
      <span className="truncate text-300 font-medium text-text-primary">{cohort.label}</span>
      {enforced ? (
        <div
          className="flex h-3 overflow-hidden rounded-full bg-core-primary-5"
          role="img"
          aria-label={`${cohort.label}: ${segments.map((segment) => `${segment.label} ${segment.count}`).join(", ")}`}
        >
          {segments.map((segment) =>
            segment.count > 0 ? (
              <div
                key={segment.label}
                style={{ width: `${(segment.count / denominator) * 100}%`, backgroundColor: segment.color }}
                title={`${segment.label}: ${segment.count}`}
              />
            ) : null,
          )}
        </div>
      ) : (
        <div className="text-200 text-text-primary-50">Not enforced</div>
      )}
      <span className="text-right text-200 text-text-primary-70">{enforced ? `${targetedCount} targeted` : "—"}</span>
    </div>
  );
};

const ConvergencePanel = ({
  title,
  cohorts,
  type,
}: {
  title: string;
  cohorts: CohortSummary[];
  type: "firmware" | "pools";
}) => {
  const firstCohort = cohorts[0];
  const legend = firstCohort
    ? type === "firmware"
      ? firmwareConvergenceSegments(firstCohort)
      : poolConvergenceSegments(firstCohort)
    : [];
  return (
    <article className="rounded-xl border border-border-5 bg-surface-base p-5">
      <h3 className="text-heading-100 text-text-primary">{title}</h3>
      <div className="mt-4 flex flex-wrap gap-x-4 gap-y-2">
        {legend.map((segment) => (
          <span key={segment.label} className="flex items-center gap-2 text-200 text-text-primary-70">
            <span className="h-2 w-2 rounded-full" style={{ backgroundColor: segment.color }} />
            {segment.label}
          </span>
        ))}
      </div>
      <div className="mt-5 flex flex-col gap-4">
        {cohorts.map((cohort) => {
          if (type === "firmware") {
            const enforced =
              cohort.firmwareTargets.some((target) => Boolean(target.firmwareFileId)) ||
              Boolean(cohort.desiredFirmwareFileId);
            return (
              <ConvergenceBar
                key={cohort.id.toString()}
                cohort={cohort}
                segments={firmwareConvergenceSegments(cohort)}
                enforced={enforced}
                targetedCount={cohort.firmwareProgress?.targetedCount ?? 0}
              />
            );
          }
          const progress = cohort.configProgress.find((item) => item.dimension === CohortConfigDimension.POOLS);
          return (
            <ConvergenceBar
              key={cohort.id.toString()}
              cohort={cohort}
              segments={poolConvergenceSegments(cohort)}
              enforced={Boolean(cohort.desiredConfig?.pools)}
              targetedCount={progress?.targetedCount ?? 0}
            />
          );
        })}
      </div>
    </article>
  );
};

type MetricKey = "hashrate" | "efficiency" | "power";

const metricDefinitions: Record<
  MetricKey,
  {
    title: string;
    unit: string;
    normalize: (value: number) => number;
    metric: CohortTelemetryComparisonMetric;
    direction: "higher" | "lower" | "neutral";
  }
> = {
  hashrate: {
    title: "Hashrate change",
    unit: "TH/s",
    normalize: (value) => normalizeHashrateToTHs(value, 1),
    metric: CohortTelemetryComparisonMetric.HASHRATE,
    direction: "higher",
  },
  efficiency: {
    title: "Efficiency change",
    unit: "J/TH",
    normalize: normalizeEfficiencyToJTH,
    metric: CohortTelemetryComparisonMetric.EFFICIENCY,
    direction: "lower",
  },
  power: {
    title: "Power change",
    unit: "kW",
    normalize: (value) => normalizePowerToKW(value, 1),
    metric: CohortTelemetryComparisonMetric.POWER,
    direction: "neutral",
  },
};

const distributionFor = (series: CohortTelemetryComparisonSeries, metric: CohortTelemetryComparisonMetric) =>
  series.distributions.find((distribution) => distribution.metric === metric);

const formatChange = (value: number) => `${value > 0 ? "+" : ""}${value.toFixed(1)}%`;

const formatAbsolute = (value: number | undefined, definition: (typeof metricDefinitions)[MetricKey]) => {
  if (value === undefined) return "—";
  const normalized = definition.normalize(value);
  return `${normalized.toFixed(Math.abs(normalized) >= 10 ? 0 : 1)} ${definition.unit}`;
};

const comparisonDomain = (series: CohortTelemetryComparisonSeries[], metric: CohortTelemetryComparisonMetric) => {
  const values = series.flatMap((cohortSeries) => {
    const distribution = distributionFor(cohortSeries, metric);
    return [
      distribution?.p25PercentageChange,
      distribution?.medianPercentageChange,
      distribution?.p75PercentageChange,
    ].filter((value): value is number => value !== undefined);
  });
  const maximum = values.length > 0 ? Math.max(...values.map((value) => Math.abs(value))) : 0;
  return Math.max(5, Math.ceil(maximum / 5) * 5);
};

const changePosition = (value: number, domain: number) =>
  Math.max(0, Math.min(100, ((value + domain) / (2 * domain)) * 100));

const changeTone = (value: number, direction: (typeof metricDefinitions)[MetricKey]["direction"]) => {
  if (direction === "neutral" || Math.abs(value) < 0.05) return "text-text-primary";
  const positive = direction === "higher" ? value > 0 : value < 0;
  return positive ? "text-intent-success-fill" : "text-intent-critical-fill";
};

const cohortSeriesColor = (series: CohortTelemetryComparisonSeries, colors: Map<string, string>) => {
  if (series.isDefault) return defaultCohortColor;
  return colors.get(series.cohortId.toString()) ?? cohortColors[Number(series.cohortId % BigInt(cohortColors.length))];
};

const DistributionRow = ({
  series,
  distribution,
  definition,
  domain,
  color,
  metricKey,
}: {
  series: CohortTelemetryComparisonSeries;
  distribution: CohortTelemetryComparisonDistribution | undefined;
  definition: (typeof metricDefinitions)[MetricKey];
  domain: number;
  color: string;
  metricKey: MetricKey;
}) => {
  const hasComparison =
    distribution?.medianPercentageChange !== undefined &&
    distribution.p25PercentageChange !== undefined &&
    distribution.p75PercentageChange !== undefined;

  return (
    <div className="border-t border-border-5 py-4 first:border-t-0 first:pt-1">
      <div className="flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ backgroundColor: color }} />
          <span className="truncate text-300 font-medium text-text-primary">{series.label}</span>
        </div>
        {hasComparison ? (
          <span
            className={`shrink-0 text-emphasis-200 ${changeTone(distribution.medianPercentageChange!, definition.direction)}`}
          >
            {formatChange(distribution.medianPercentageChange!)}
          </span>
        ) : (
          <span className="shrink-0 text-200 text-text-primary-50">No comparison</span>
        )}
      </div>

      {hasComparison ? (
        <>
          <div
            className="relative mt-4 h-6"
            aria-label={`${series.label}: ${formatChange(distribution.medianPercentageChange!)} median change`}
          >
            <div className="absolute top-2.5 h-1 w-full rounded-full bg-core-primary-5" />
            <div className="absolute top-0 bottom-0 left-1/2 w-px bg-border-10" />
            <div
              className="absolute top-2 h-2 rounded-full opacity-40"
              style={{
                left: `${changePosition(distribution.p25PercentageChange!, domain)}%`,
                width: `${Math.max(1, changePosition(distribution.p75PercentageChange!, domain) - changePosition(distribution.p25PercentageChange!, domain))}%`,
                backgroundColor: color,
              }}
              title={`Middle 50%: ${formatChange(distribution.p25PercentageChange!)} to ${formatChange(distribution.p75PercentageChange!)}`}
            />
            <span
              className="absolute top-1 h-4 w-1 -translate-x-1/2 rounded-full"
              style={{
                left: `${changePosition(distribution.medianPercentageChange!, domain)}%`,
                backgroundColor: color,
              }}
            />
          </div>
          <div className="mt-1 flex flex-wrap items-center justify-between gap-x-2 gap-y-1 text-200 text-text-primary-70">
            <span>
              {formatAbsolute(distribution.baselineMedian, definition)} →{" "}
              {formatAbsolute(distribution.comparisonMedian, definition)}
            </span>
            <span>
              {distribution.eligibleDeviceCount}/{series.memberCount.toString()} comparable
            </span>
          </div>
          {metricKey === "hashrate" && series.currentNonHashingDeviceCount > 0 ? (
            <p className="mt-1 text-200 text-intent-warning-fill">
              {series.currentNonHashingDeviceCount} {series.currentNonHashingDeviceCount === 1 ? "miner" : "miners"} not
              hashing
            </p>
          ) : null}
          {distribution.zeroBaselineDeviceCount > 0 ? (
            <p className="mt-1 text-200 text-text-primary-50">
              {distribution.zeroBaselineDeviceCount} zero-baseline{" "}
              {distribution.zeroBaselineDeviceCount === 1 ? "miner" : "miners"} excluded
            </p>
          ) : null}
          {metricKey === "efficiency" &&
          series.baselineAggregateEfficiency !== undefined &&
          series.comparisonAggregateEfficiency !== undefined ? (
            <p className="mt-2 text-200 text-text-primary-70">
              Aggregate efficiency {formatAbsolute(series.baselineAggregateEfficiency, definition)} →{" "}
              {formatAbsolute(series.comparisonAggregateEfficiency, definition)}
              {series.aggregateEfficiencyPercentageChange === undefined
                ? ""
                : ` (${formatChange(series.aggregateEfficiencyPercentageChange)})`}{" "}
              · {series.aggregateEfficiencyDeviceCount}/{series.memberCount.toString()} paired
            </p>
          ) : null}
        </>
      ) : (
        <p className="mt-3 text-200 text-text-primary-70">
          {distribution
            ? `${distribution.currentReportingDeviceCount}/${series.memberCount.toString()} report now; a matching nonzero baseline is required.`
            : "No telemetry is available for these windows."}
        </p>
      )}
    </div>
  );
};

const OutcomePanel = ({
  metricKey,
  comparison,
  selectedCohorts,
}: {
  metricKey: MetricKey;
  comparison: GetCohortTelemetryComparisonResponse;
  selectedCohorts: CohortSummary[];
}) => {
  const definition = metricDefinitions[metricKey];
  const colorByID = new Map(selectedCohorts.map((cohort) => [cohort.id.toString(), cohortColor(cohort)]));
  const domain = comparisonDomain(comparison.series, definition.metric);

  return (
    <article
      className="min-w-0 rounded-xl border border-border-5 bg-surface-base p-5"
      data-testid={`cohort-${metricKey}`}
    >
      <h3 className="text-heading-100 text-text-primary">{definition.title}</h3>
      <p className="mt-1 text-200 text-text-primary-70">Median per-miner change · middle 50%</p>
      {comparison.series.length > 0 ? (
        <>
          <div className="text-100 mt-4 flex justify-between text-text-primary-50">
            <span>−{domain}%</span>
            <span>No change</span>
            <span>+{domain}%</span>
          </div>
          <div className="mt-2">
            {comparison.series.map((series) => (
              <DistributionRow
                key={series.cohortId.toString()}
                series={series}
                distribution={distributionFor(series, definition.metric)}
                definition={definition}
                domain={domain}
                color={cohortSeriesColor(series, colorByID)}
                metricKey={metricKey}
              />
            ))}
          </div>
        </>
      ) : (
        <div className="mt-4 flex min-h-48 items-center justify-center rounded-lg bg-core-primary-5 px-4 text-center text-300 text-text-primary-70">
          No comparable miner data for these windows.
        </div>
      )}
    </article>
  );
};

interface CohortComparisonDashboardProps {
  cohorts: CohortSummary[];
  selectedIds: string[];
  comparisonWindow: CohortTelemetryComparisonWindow;
  comparison: GetCohortTelemetryComparisonResponse | null;
  comparisonLoading: boolean;
  comparisonError: boolean;
  onToggleCohort: (cohortId: string) => void;
  onWindowChange: (window: CohortTelemetryComparisonWindow) => void;
}

const CohortComparisonDashboard = ({
  cohorts,
  selectedIds,
  comparisonWindow,
  comparison,
  comparisonLoading,
  comparisonError,
  onToggleCohort,
  onWindowChange,
}: CohortComparisonDashboardProps) => {
  const selectedCohorts = useMemo(
    () =>
      selectedIds.map((id) => cohorts.find((cohort) => cohort.id.toString() === id)).filter(Boolean) as CohortSummary[],
    [cohorts, selectedIds],
  );
  const windowLabel =
    comparisonWindowOptions.find((option) => option.value === comparisonWindow)?.label ?? "selected window";

  return (
    <div className="flex flex-col gap-8" data-testid="cohort-comparison-dashboard">
      <FleetAllocation cohorts={cohorts} />
      <CohortSelector cohorts={cohorts} selectedIds={selectedIds} onToggle={onToggleCohort} />

      <section>
        <SectionHeading heading="Desired-state convergence" />
        <p className="mt-1 text-300 text-text-primary-70">
          Compare how each selected cohort is progressing toward its firmware and pool targets.
        </p>
        <div className="mt-4 grid gap-4 desktop:grid-cols-2">
          <ConvergencePanel title="Firmware" cohorts={selectedCohorts} type="firmware" />
          <ConvergencePanel title="Pools" cohorts={selectedCohorts} type="pools" />
        </div>
      </section>

      <section>
        <SectionHeading heading="Operating outcomes">
          <div className="flex rounded-lg border border-border-5 bg-surface-base p-1">
            {comparisonWindowOptions.map((option) => (
              <button
                key={option.value}
                type="button"
                className={`rounded-md px-3 py-1.5 text-emphasis-200 ${
                  comparisonWindow === option.value
                    ? "bg-core-primary-fill text-text-contrast"
                    : "text-text-primary-70 hover:bg-core-primary-5"
                }`}
                onClick={() => onWindowChange(option.value)}
              >
                {option.label}
              </button>
            ))}
          </div>
        </SectionHeading>
        <p className="mt-1 text-300 text-text-primary-70">
          Each miner's latest {windowLabel} is compared with its own preceding {windowLabel}, then each cohort is
          summarized by its median and middle 50%. This controls for differences in miner model, bin, and baseline
          performance.
        </p>
        {comparisonError ? (
          <div className="mt-4">
            <Callout
              intent="danger"
              prefixIcon={<Alert />}
              title="Couldn't load operating outcomes"
              subtitle="The cohort register and convergence data are still available."
            />
          </div>
        ) : comparisonLoading || !comparison ? (
          <div className="mt-4 grid gap-4 desktop:grid-cols-3">
            {["hashrate", "efficiency", "power"].map((key) => (
              <div key={key} className="rounded-xl border border-border-5 bg-surface-base p-5">
                <SkeletonBar className="h-5 w-32" />
                <SkeletonBar className="mt-5 h-56" />
              </div>
            ))}
          </div>
        ) : (
          <div className="mt-4 grid gap-4 desktop:grid-cols-3">
            <OutcomePanel metricKey="hashrate" comparison={comparison} selectedCohorts={selectedCohorts} />
            <OutcomePanel metricKey="efficiency" comparison={comparison} selectedCohorts={selectedCohorts} />
            <OutcomePanel metricKey="power" comparison={comparison} selectedCohorts={selectedCohorts} />
          </div>
        )}
      </section>
    </div>
  );
};

export default CohortComparisonDashboard;
