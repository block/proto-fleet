import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  CohortConfigDimension,
  CohortConfigProgressSchema,
  CohortFirmwareProgressSchema,
  type CohortSummary,
  CohortSummarySchema,
  CohortTelemetryComparisonDistributionSchema,
  CohortTelemetryComparisonMetric,
  CohortTelemetryComparisonSeriesSchema,
  CohortTelemetryComparisonWindow,
  GetCohortTelemetryComparisonResponseSchema,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import {
  firmwareConvergenceSegments,
  poolConvergenceSegments,
} from "@/protoFleet/features/cohorts/components/cohortComparison";
import CohortComparisonDashboard from "@/protoFleet/features/cohorts/components/CohortComparisonDashboard";

const cohort = (id: bigint, label: string, isDefault = false): CohortSummary =>
  create(CohortSummarySchema, {
    id,
    label,
    isDefault,
    explicitMemberCount: isDefault ? 8n : 2n,
  });

describe("CohortComparisonDashboard", () => {
  it("groups firmware and pool states into cohort-specific convergence segments", () => {
    const summary = create(CohortSummarySchema, {
      id: 42n,
      label: "Rollout A",
      firmwareProgress: create(CohortFirmwareProgressSchema, {
        completeCount: 4,
        queuedCount: 1,
        updatingCount: 2,
        verifyingCount: 3,
        needsAttentionCount: 2,
        unknownCount: 1,
      }),
      configProgress: [
        create(CohortConfigProgressSchema, {
          dimension: CohortConfigDimension.POOLS,
          convergedCount: 5,
          waitingCount: 1,
          applyingCount: 2,
          verifyingCount: 1,
          heldCount: 2,
          failedCount: 1,
          unsupportedCount: 3,
        }),
      ],
    });

    expect(firmwareConvergenceSegments(summary).map(({ label, count }) => [label, count])).toEqual([
      ["Complete", 4],
      ["In progress", 6],
      ["Needs attention", 2],
      ["Unknown", 1],
    ]);
    expect(poolConvergenceSegments(summary).map(({ label, count }) => [label, count])).toEqual([
      ["Converged", 5],
      ["In progress", 4],
      ["Held", 2],
      ["Failed", 1],
      ["Unsupported", 3],
    ]);
  });

  it("pins the default cohort, caps selection at five, and renders section-local no-data states", () => {
    const cohorts = [
      cohort(1n, "Default", true),
      cohort(2n, "A"),
      cohort(3n, "B"),
      cohort(4n, "C"),
      cohort(5n, "D"),
      cohort(6n, "E"),
    ];
    const onToggle = vi.fn();
    render(
      <CohortComparisonDashboard
        cohorts={cohorts}
        selectedIds={["1", "2", "3", "4", "5"]}
        comparisonWindow={CohortTelemetryComparisonWindow.SIX_HOURS}
        comparison={create(GetCohortTelemetryComparisonResponseSchema, {
          comparisonWindow: CohortTelemetryComparisonWindow.SIX_HOURS,
        })}
        comparisonLoading={false}
        comparisonError={false}
        onToggleCohort={onToggle}
        onWindowChange={vi.fn()}
      />,
    );

    const selector = within(screen.getByTestId("cohort-comparison-selector"));
    const defaultCheckbox = selector.getByText("Default").closest("label")?.querySelector("input");
    const sixthCheckbox = selector.getByText("E").closest("label")?.querySelector("input");
    expect(defaultCheckbox).toBeDisabled();
    expect(defaultCheckbox).toBeChecked();
    expect(sixthCheckbox).toBeDisabled();
    expect(screen.getAllByText("No comparable miner data for these windows.")).toHaveLength(3);

    fireEvent.click(selector.getByText("A"));
    expect(onToggle).toHaveBeenCalledWith("2");
  });

  it("keeps cohort colors stable when the cohort list changes", () => {
    const defaultCohort = cohort(1n, "Default", true);
    const existingCohort = cohort(2n, "Existing");
    const comparison = create(GetCohortTelemetryComparisonResponseSchema, {
      comparisonWindow: CohortTelemetryComparisonWindow.SIX_HOURS,
    });
    const dashboardProps = {
      selectedIds: ["1", "2"],
      comparisonWindow: CohortTelemetryComparisonWindow.SIX_HOURS,
      comparison,
      comparisonLoading: false,
      comparisonError: false,
      onToggleCohort: vi.fn(),
      onWindowChange: vi.fn(),
    };
    const { container, rerender } = render(
      <CohortComparisonDashboard cohorts={[defaultCohort, existingCohort]} {...dashboardProps} />,
    );
    const initialColor = container.querySelector<HTMLElement>('[data-cohort-id="2"]')?.style.backgroundColor;

    rerender(
      <CohortComparisonDashboard
        cohorts={[defaultCohort, cohort(99n, "New cohort"), existingCohort]}
        {...dashboardProps}
      />,
    );

    expect(container.querySelector<HTMLElement>('[data-cohort-id="2"]')?.style.backgroundColor).toBe(initialColor);
  });

  it("shows paired per-miner distributions, non-hashing miners, and aggregate efficiency", () => {
    const distribution = (
      metric: CohortTelemetryComparisonMetric,
      baselineMedian: number,
      comparisonMedian: number,
      medianPercentageChange: number,
      eligibleDeviceCount: number,
    ) =>
      create(CohortTelemetryComparisonDistributionSchema, {
        metric,
        baselineMedian,
        comparisonMedian,
        medianPercentageChange,
        p25PercentageChange: medianPercentageChange - 4,
        p75PercentageChange: medianPercentageChange + 4,
        eligibleDeviceCount,
        baselineReportingDeviceCount: eligibleDeviceCount,
        currentReportingDeviceCount: eligibleDeviceCount,
      });
    const comparison = create(GetCohortTelemetryComparisonResponseSchema, {
      comparisonWindow: CohortTelemetryComparisonWindow.SIX_HOURS,
      series: [
        create(CohortTelemetryComparisonSeriesSchema, {
          cohortId: 1n,
          label: "Default",
          isDefault: true,
          memberCount: 8n,
          currentNonHashingDeviceCount: 2,
          baselineAggregateEfficiency: 26e-12,
          comparisonAggregateEfficiency: 24e-12,
          aggregateEfficiencyPercentageChange: -7.692,
          aggregateEfficiencyDeviceCount: 8,
          distributions: [
            distribution(CohortTelemetryComparisonMetric.HASHRATE, 100e12, 110e12, 10, 8),
            distribution(CohortTelemetryComparisonMetric.EFFICIENCY, 25e-12, 23e-12, -8, 8),
            distribution(CohortTelemetryComparisonMetric.POWER, 3200, 3300, 3.125, 8),
          ],
        }),
        create(CohortTelemetryComparisonSeriesSchema, {
          cohortId: 2n,
          label: "Rollout A",
          memberCount: 2n,
          distributions: [
            distribution(CohortTelemetryComparisonMetric.HASHRATE, 90e12, 85e12, -5.556, 2),
            distribution(CohortTelemetryComparisonMetric.EFFICIENCY, 28e-12, 27e-12, -3.571, 2),
            distribution(CohortTelemetryComparisonMetric.POWER, 3000, 2900, -3.333, 2),
          ],
        }),
      ],
    });

    render(
      <CohortComparisonDashboard
        cohorts={[cohort(1n, "Default", true), cohort(2n, "Rollout A")]}
        selectedIds={["1", "2"]}
        comparisonWindow={CohortTelemetryComparisonWindow.SIX_HOURS}
        comparison={comparison}
        comparisonLoading={false}
        comparisonError={false}
        onToggleCohort={vi.fn()}
        onWindowChange={vi.fn()}
      />,
    );

    const hashrate = within(screen.getByTestId("cohort-hashrate"));
    expect(hashrate.getByLabelText("Default: +10.0% median change")).toBeInTheDocument();
    expect(hashrate.getByText("2 miners not hashing")).toBeInTheDocument();
    expect(hashrate.getByText("+10.0%")).toHaveClass("text-intent-success-fill");
    expect(hashrate.getByText("-5.6%")).toHaveClass("text-intent-critical-fill");
    expect(hashrate.getByText("8/8 comparable")).toBeInTheDocument();

    const efficiency = within(screen.getByTestId("cohort-efficiency"));
    expect(efficiency.getByText("Aggregate efficiency 26 J/TH → 24 J/TH (-7.7%) · 8/8 paired")).toBeInTheDocument();
    expect(
      screen.getByText(/controls for differences in miner model, bin, and baseline performance/),
    ).toBeInTheDocument();
  });
});
