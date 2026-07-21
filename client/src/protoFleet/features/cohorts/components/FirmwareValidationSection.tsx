import { useEffect, useMemo, useState } from "react";
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import clsx from "clsx";
import { durationMs, timestampMs } from "@bufbuild/protobuf/wkt";

import {
  type Cohort,
  type CohortFirmwareTarget,
  type CohortFirmwareValidationBaseline,
  type CohortFirmwareValidationMetric,
  CohortFirmwareValidationState,
  CohortFirmwareValidationWindow,
  type GetCohortFirmwareValidationResponse,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useCohortApi } from "@/protoFleet/api/useCohortApi";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import SectionHeading from "@/protoFleet/features/dashboard/components/SectionHeading";
import {
  normalizeEfficiencyToJTH,
  normalizeHashrateToTHs,
} from "@/protoFleet/features/dashboard/utils/metricNormalization";
import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import Select from "@/shared/components/Select";
import SkeletonBar from "@/shared/components/SkeletonBar";

const defaultWindow = CohortFirmwareValidationWindow.SIX_HOURS;

const windowOptions = [
  { value: String(CohortFirmwareValidationWindow.ONE_HOUR), label: "1 hour" },
  { value: String(CohortFirmwareValidationWindow.SIX_HOURS), label: "6 hours" },
  { value: String(CohortFirmwareValidationWindow.TWENTY_FOUR_HOURS), label: "24 hours" },
];

const targetKey = (target: CohortFirmwareTarget) =>
  `${target.manufacturer.trim().toLocaleLowerCase()}:::${target.model.trim().toLocaleLowerCase()}`;

const targetLabel = (target: CohortFirmwareTarget) => `${target.manufacturer} ${target.model}`.trim();

const comparisonStateMessage = (state: CohortFirmwareValidationState) => {
  switch (state) {
    case CohortFirmwareValidationState.NO_TARGET:
      return {
        title: "Set a firmware target to compare outcomes",
        detail: "Validation begins after this cohort has a model-specific firmware target.",
      };
    case CohortFirmwareValidationState.TARGET_VERSION_UNKNOWN:
      return {
        title: "The target firmware version is unknown",
        detail: "Add version metadata to the selected firmware file before comparing outcomes.",
      };
    case CohortFirmwareValidationState.NO_BASELINE:
      return {
        title: "No previous firmware baseline is available",
        detail: "The selected miners were already on the target or did not have a trustworthy version observation.",
      };
    case CohortFirmwareValidationState.STABILIZING:
      return {
        title: "Collecting stable target telemetry",
        detail: "Results will appear after completed miners finish the selected stabilization and comparison window.",
      };
    case CohortFirmwareValidationState.HISTORY_EXPIRED:
      return {
        title: "The baseline is outside telemetry retention",
        detail: "This rollout began more than three months ago, so comparable hourly telemetry is unavailable.",
      };
    case CohortFirmwareValidationState.INSUFFICIENT_TELEMETRY:
      return {
        title: "Not enough comparable telemetry",
        detail: "Neither the baseline nor target window has enough reporting data for this firmware comparison.",
      };
    default:
      return null;
  }
};

const metricDefinitions = {
  [MeasurementType.HASHRATE]: {
    testId: "hashrate",
    title: "Hashrate",
    units: "TH/s",
    positiveDirection: "higher" as const,
    normalize: (value: number, deviceCount: number) => normalizeHashrateToTHs(value, deviceCount),
  },
  [MeasurementType.EFFICIENCY]: {
    testId: "efficiency",
    title: "Efficiency",
    units: "J/TH",
    positiveDirection: "lower" as const,
    normalize: (value: number) => normalizeEfficiencyToJTH(value),
  },
  [MeasurementType.POWER]: {
    testId: "power",
    title: "Power / miner",
    units: "kW",
    positiveDirection: "neutral" as const,
    normalize: (value: number) => (Math.abs(value) > 100 ? value / 1_000 : value),
  },
};

type MetricDefinition = (typeof metricDefinitions)[keyof typeof metricDefinitions];

type ValidationChartDatum = {
  elapsed: number;
  baseline: number | null;
  target: number | null;
};

const formatElapsed = (milliseconds: number) => {
  const totalMinutes = Math.round(milliseconds / 60_000);
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  if (hours === 0) return `${minutes}m`;
  if (minutes === 0) return `${hours}h`;
  return `${hours}h ${minutes}m`;
};

const formatMetricValue = (value: number | undefined, units: string) =>
  value === undefined ? "No data" : `${value.toFixed(1)} ${units}`;

const normalizeMetricValue = (definition: MetricDefinition, value: number | undefined, deviceCount: number) =>
  value === undefined ? undefined : definition.normalize(value, deviceCount);

const buildChartData = (metric: CohortFirmwareValidationMetric, definition: MetricDefinition) => {
  const points = new Map<number, ValidationChartDatum>();
  for (const point of metric.baselinePoints) {
    const elapsed = point.elapsed ? durationMs(point.elapsed) : 0;
    points.set(elapsed, {
      elapsed,
      baseline: definition.normalize(point.value, point.deviceCount),
      target: points.get(elapsed)?.target ?? null,
    });
  }
  for (const point of metric.targetPoints) {
    const elapsed = point.elapsed ? durationMs(point.elapsed) : 0;
    const existing = points.get(elapsed);
    points.set(elapsed, {
      elapsed,
      baseline: existing?.baseline ?? null,
      target: definition.normalize(point.value, point.deviceCount),
    });
  }
  return [...points.values()].sort((a, b) => a.elapsed - b.elapsed);
};

const deltaClassName = (delta: number | undefined, direction: MetricDefinition["positiveDirection"]) => {
  if (delta === undefined || delta === 0 || direction === "neutral") return "text-text-primary-70";
  const positive = direction === "higher" ? delta > 0 : delta < 0;
  return positive ? "text-intent-success-fill" : "text-intent-critical-fill";
};

const ValidationMetricPanel = ({
  metric,
  eligibleCount,
}: {
  metric: CohortFirmwareValidationMetric | undefined;
  eligibleCount: number;
}) => {
  const definition = metric ? metricDefinitions[metric.measurementType as keyof typeof metricDefinitions] : undefined;
  if (!metric || !definition) return null;

  const baselineAverage = normalizeMetricValue(definition, metric.baselineAverage, metric.baselineReportingDeviceCount);
  const targetAverage = normalizeMetricValue(definition, metric.targetAverage, metric.targetReportingDeviceCount);
  const absoluteDelta =
    baselineAverage === undefined || targetAverage === undefined ? undefined : targetAverage - baselineAverage;
  const chartData = buildChartData(metric, definition);
  const deltaLabel =
    absoluteDelta === undefined
      ? "No delta"
      : `${absoluteDelta > 0 ? "+" : ""}${absoluteDelta.toFixed(1)} ${definition.units}${
          metric.percentageDelta === undefined
            ? ""
            : ` (${metric.percentageDelta > 0 ? "+" : ""}${metric.percentageDelta.toFixed(1)}%)`
        }`;

  return (
    <article
      className="min-w-0 rounded-xl border border-border-5 bg-surface-base p-5"
      data-testid={`validation-metric-${definition.testId}`}
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-heading-100 text-text-primary">{definition.title}</h3>
          <p className="mt-1 text-200 text-text-primary-70">
            {metric.baselineReportingDeviceCount}/{eligibleCount} baseline · {metric.targetReportingDeviceCount}/
            {eligibleCount} target reporting
          </p>
        </div>
        <div
          className={clsx("text-right text-emphasis-300", deltaClassName(absoluteDelta, definition.positiveDirection))}
        >
          {deltaLabel}
        </div>
      </div>

      <div className="mt-5 grid grid-cols-2 gap-3 text-200">
        <div className="rounded-lg bg-core-primary-5 p-3">
          <div className="text-text-primary-50">Previous firmware</div>
          <div className="mt-1 text-emphasis-300 text-text-primary">
            {formatMetricValue(baselineAverage, definition.units)}
          </div>
        </div>
        <div className="rounded-lg bg-core-primary-5 p-3">
          <div className="text-text-primary-50">Target firmware</div>
          <div className="mt-1 text-emphasis-300 text-text-primary">
            {formatMetricValue(targetAverage, definition.units)}
          </div>
        </div>
      </div>

      {chartData.length > 0 ? (
        <div
          className="mt-5 h-48"
          aria-label={`${definition.title} previous and target firmware comparison`}
          role="img"
        >
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={chartData} margin={{ top: 8, right: 8, bottom: 0, left: -16 }}>
              <CartesianGrid stroke="var(--color-border-5)" vertical={false} />
              <XAxis
                dataKey="elapsed"
                type="number"
                domain={["dataMin", "dataMax"]}
                tickFormatter={formatElapsed}
                tick={{ fill: "var(--color-text-primary)", fillOpacity: 0.5, fontSize: 12 }}
                axisLine={false}
                tickLine={false}
              />
              <YAxis
                tick={{ fill: "var(--color-text-primary)", fillOpacity: 0.5, fontSize: 12 }}
                axisLine={false}
                tickLine={false}
                width={56}
              />
              <Tooltip
                labelFormatter={(value) => formatElapsed(Number(value))}
                formatter={(value, name) => [
                  `${Number(value).toFixed(1)} ${definition.units}`,
                  name === "baseline" ? "Previous firmware" : "Target firmware",
                ]}
              />
              <Line
                type="monotone"
                dataKey="baseline"
                stroke="var(--color-core-primary-50)"
                strokeWidth={2}
                strokeDasharray="5 4"
                dot={false}
                connectNulls
              />
              <Line
                type="monotone"
                dataKey="target"
                stroke="var(--color-core-primary-fill)"
                strokeWidth={2}
                dot={false}
                connectNulls
              />
            </LineChart>
          </ResponsiveContainer>
        </div>
      ) : (
        <div className="mt-5 rounded-lg bg-core-primary-5 px-4 py-8 text-center text-300 text-text-primary-70">
          No comparable {definition.title.toLocaleLowerCase()} data
        </div>
      )}
    </article>
  );
};

const intervalLabel = (baseline: CohortFirmwareValidationBaseline) => {
  if (!baseline.baselineStartTime || !baseline.targetStartTime) return null;
  const formatter = new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
  return `Baseline ${formatter.format(timestampMs(baseline.baselineStartTime))} · Target ${formatter.format(
    timestampMs(baseline.targetStartTime),
  )}`;
};

const exclusionSummary = (response: GetCohortFirmwareValidationResponse) => {
  const exclusions = response.exclusions;
  if (!exclusions) return "";
  const parts = [
    [exclusions.addedAfterRolloutCount, "added after rollout"],
    [exclusions.unknownBaselineCount, "without a baseline"],
    [exclusions.alreadyOnTargetCount, "already on target"],
    [exclusions.incompleteCount, "incomplete"],
    [exclusions.stabilizingCount, "stabilizing"],
    [exclusions.untrustedTransitionCount, "without a trustworthy transition"],
  ]
    .filter(([count]) => Number(count) > 0)
    .map(([count, label]) => `${count} ${label}`);
  return parts.length > 0 ? `Excluded: ${parts.join(", ")}.` : "";
};

const FirmwareValidationSection = ({ cohort }: { cohort: Cohort }) => {
  const summary = cohort.summary;
  const targets = useMemo(
    () => cohort.firmwareTargets.filter((target) => target.firmwareFileId),
    [cohort.firmwareTargets],
  );
  const [selectedTargetKey, setSelectedTargetKey] = useState(() => (targets[0] ? targetKey(targets[0]) : ""));
  const [comparisonWindow, setComparisonWindow] = useState(defaultWindow);
  const [loadedResponse, setLoadedResponse] = useState<{
    key: string;
    response: GetCohortFirmwareValidationResponse;
  } | null>(null);
  const [selectedBaselineVersion, setSelectedBaselineVersion] = useState("");
  const [loadingKey, setLoadingKey] = useState<string | null>(null);
  const [errorKey, setErrorKey] = useState<string | null>(null);
  const { getFirmwareValidation } = useCohortApi();

  const selectedTarget = targets.find((target) => targetKey(target) === selectedTargetKey) ?? targets[0];
  const effectiveTargetKey = selectedTarget ? targetKey(selectedTarget) : "";
  const validationRefreshKey = useMemo(
    () =>
      cohort.members
        .map(
          (member) =>
            `${member.deviceIdentifier}:${member.firmwareStatus?.state ?? ""}:${member.firmwareStatus?.currentFirmwareVersion ?? ""}`,
        )
        .sort()
        .join("|"),
    [cohort.members],
  );
  const requestKey =
    summary && selectedTarget ? `${summary.id}:${effectiveTargetKey}:${comparisonWindow}:${validationRefreshKey}` : "";
  const response = loadedResponse?.key === requestKey ? loadedResponse.response : null;
  const isLoading = requestKey !== "" && loadingKey === requestKey;
  const hasError = requestKey !== "" && errorKey === requestKey;

  useEffect(() => {
    if (!summary || !selectedTarget || requestKey === "") return undefined;
    let cancelled = false;
    let hasLoaded = false;
    const load = async () => {
      if (!hasLoaded) setLoadingKey(requestKey);
      setErrorKey(null);
      try {
        const next = await getFirmwareValidation({
          cohortId: summary.id,
          manufacturer: selectedTarget.manufacturer,
          model: selectedTarget.model,
          comparisonWindow,
        });
        if (!cancelled) {
          setLoadedResponse({ key: requestKey, response: next });
          hasLoaded = true;
        }
      } catch {
        if (!cancelled) setErrorKey(requestKey);
      } finally {
        if (!cancelled) setLoadingKey((current) => (current === requestKey ? null : current));
      }
    };
    void load();
    const interval = window.setInterval(() => void load(), POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(interval);
    };
  }, [comparisonWindow, getFirmwareValidation, requestKey, selectedTarget, summary]);

  if (!summary) return null;

  const preferredBaseline = response?.baselines.reduce<CohortFirmwareValidationBaseline | undefined>(
    (largest, baseline) => (!largest || baseline.eligibleCount > largest.eligibleCount ? baseline : largest),
    undefined,
  );
  const effectiveBaselineVersion = response?.baselines.some(
    (baseline) => baseline.previousFirmwareVersion === selectedBaselineVersion,
  )
    ? selectedBaselineVersion
    : (preferredBaseline?.previousFirmwareVersion ?? "");
  const selectedBaseline = response?.baselines.find(
    (baseline) => baseline.previousFirmwareVersion === effectiveBaselineVersion,
  );
  const displayedState = selectedBaseline?.state ?? response?.state;
  const stateMessage = displayedState === undefined ? null : comparisonStateMessage(displayedState);
  const targetOptions = targets.map((target) => ({ value: targetKey(target), label: targetLabel(target) }));
  const baselineOptions =
    response?.baselines.map((baseline) => ({
      value: baseline.previousFirmwareVersion,
      label: `${baseline.previousFirmwareVersion} · ${baseline.eligibleCount}/${baseline.memberCount} eligible`,
    })) ?? [];
  const interval = selectedBaseline ? intervalLabel(selectedBaseline) : null;

  return (
    <section data-testid="firmware-validation-section">
      <SectionHeading heading="Validation outcomes" />

      <div className="mt-4 grid gap-3 tablet:grid-cols-3">
        {targets.length > 1 ? (
          <Select
            id="firmware-validation-target"
            label="Miner target"
            options={targetOptions}
            value={effectiveTargetKey}
            onChange={setSelectedTargetKey}
          />
        ) : (
          <div className="flex h-14 flex-col justify-center rounded-lg border border-border-5 bg-surface-base px-4">
            <span className="text-200 text-text-primary-50">Miner target</span>
            <span className="truncate text-300 text-text-primary">
              {selectedTarget ? targetLabel(selectedTarget) : "No firmware target"}
            </span>
          </div>
        )}
        <Select
          id="firmware-validation-baseline"
          label="Previous firmware"
          options={baselineOptions}
          value={effectiveBaselineVersion}
          onChange={setSelectedBaselineVersion}
          disabled={baselineOptions.length <= 1}
          placeholder={isLoading ? "Loading baselines…" : "No baseline"}
        />
        <Select
          id="firmware-validation-window"
          label="Comparison window"
          options={windowOptions}
          value={String(comparisonWindow)}
          onChange={(value) => setComparisonWindow(Number(value) as CohortFirmwareValidationWindow)}
        />
      </div>

      {hasError ? (
        <div className="mt-4">
          <Callout intent="danger" prefixIcon={<Alert />} title="Couldn't load firmware validation" />
        </div>
      ) : null}

      {isLoading && !response ? (
        <div className="mt-4 grid gap-4 desktop:grid-cols-3" data-testid="firmware-validation-loading">
          {["hashrate", "efficiency", "power"].map((metric) => (
            <div key={metric} className="rounded-xl border border-border-5 bg-surface-base p-5">
              <SkeletonBar className="h-64 w-full" />
            </div>
          ))}
        </div>
      ) : null}

      {!isLoading && !hasError && targets.length === 0 ? (
        <div className="mt-4 rounded-xl border border-border-5 bg-surface-base px-6 py-10 text-center">
          <h3 className="text-heading-100 text-text-primary">Set a firmware target to compare outcomes</h3>
          <p className="mt-2 text-300 text-text-primary-70">
            Validation begins after this cohort has a model-specific firmware target.
          </p>
        </div>
      ) : null}

      {!isLoading && !hasError && response && stateMessage ? (
        <div
          className="mt-4 rounded-xl border border-border-5 bg-surface-base px-6 py-10 text-center"
          data-testid="firmware-validation-empty-state"
        >
          <h3 className="text-heading-100 text-text-primary">{stateMessage.title}</h3>
          <p className="mx-auto mt-2 max-w-2xl text-300 text-text-primary-70">{stateMessage.detail}</p>
          {exclusionSummary(response) ? (
            <p className="mx-auto mt-3 max-w-2xl text-200 text-text-primary-50">{exclusionSummary(response)}</p>
          ) : null}
        </div>
      ) : null}

      {!hasError && response && selectedBaseline?.state === CohortFirmwareValidationState.AVAILABLE ? (
        <>
          <div className="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-lg bg-core-primary-5 px-4 py-3 text-200">
            <div className="text-text-primary-70">
              Comparing{" "}
              <span className="font-medium text-text-primary">{selectedBaseline.previousFirmwareVersion}</span> with{" "}
              <span className="font-medium text-text-primary">{response.targetFirmwareVersion}</span> ·{" "}
              {selectedBaseline.eligibleCount} of {response.targetedCount} targeted miners eligible
            </div>
            <div className="flex flex-wrap items-center gap-3">
              {response.preliminary ? (
                <span className="rounded-full bg-intent-warning-20 px-3 py-1 text-emphasis-200 text-text-primary">
                  Preliminary
                </span>
              ) : (
                <span className="rounded-full bg-intent-success-20 px-3 py-1 text-emphasis-200 text-text-primary">
                  Fully converged
                </span>
              )}
              {interval ? <span className="text-text-primary-50">{interval}</span> : null}
            </div>
          </div>
          {exclusionSummary(response) ? (
            <p className="mt-2 text-200 text-text-primary-50">{exclusionSummary(response)}</p>
          ) : null}
          <div className="mt-4 grid grid-cols-1 gap-4 desktop:grid-cols-3">
            {[MeasurementType.HASHRATE, MeasurementType.EFFICIENCY, MeasurementType.POWER].map((measurementType) => (
              <ValidationMetricPanel
                key={measurementType}
                metric={selectedBaseline.metrics.find((metric) => metric.measurementType === measurementType)}
                eligibleCount={selectedBaseline.eligibleCount}
              />
            ))}
          </div>
        </>
      ) : null}
    </section>
  );
};

export default FirmwareValidationSection;
