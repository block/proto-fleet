import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import FirmwareValidationSection from "./FirmwareValidationSection";
import {
  CohortFirmwareTargetSchema,
  CohortFirmwareValidationBaselineSchema,
  CohortFirmwareValidationMetricSchema,
  CohortFirmwareValidationState,
  CohortFirmwareValidationWindow,
  CohortSchema,
  CohortState,
  CohortSummarySchema,
  GetCohortFirmwareValidationResponseSchema,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

const mocks = vi.hoisted(() => ({
  getFirmwareValidation: vi.fn(),
}));

vi.mock("@/protoFleet/api/useCohortApi", () => ({
  useCohortApi: () => ({ getFirmwareValidation: mocks.getFirmwareValidation }),
}));

vi.mock("@/shared/components/Select", () => ({
  default: ({
    label,
    options,
    value,
    onChange,
    disabled,
  }: {
    label: string;
    options: Array<{ value: string; label: string }>;
    value: string;
    onChange: (value: string) => void;
    disabled?: boolean;
  }) => (
    <select aria-label={label} value={value} disabled={disabled} onChange={(event) => onChange(event.target.value)}>
      {options.map((option) => (
        <option key={option.value} value={option.value}>
          {option.label}
        </option>
      ))}
    </select>
  ),
}));

const cohort = create(CohortSchema, {
  summary: create(CohortSummarySchema, {
    id: 42n,
    label: "Validation cohort",
    state: CohortState.ACTIVE,
    explicitMemberCount: 4n,
  }),
  firmwareTargets: [
    create(CohortFirmwareTargetSchema, {
      manufacturer: "Proto",
      model: "Rig",
      firmwareFileId: "fw-proto",
    }),
    create(CohortFirmwareTargetSchema, {
      manufacturer: "Bitmain",
      model: "S21",
      firmwareFileId: "fw-s21",
    }),
  ],
});

const availableResponse = create(GetCohortFirmwareValidationResponseSchema, {
  state: CohortFirmwareValidationState.AVAILABLE,
  manufacturer: "Proto",
  model: "Rig",
  targetFirmwareVersion: "2.0.0",
  comparisonWindow: CohortFirmwareValidationWindow.SIX_HOURS,
  targetedCount: 4,
  completeCount: 3,
  preliminary: true,
  baselines: [
    create(CohortFirmwareValidationBaselineSchema, {
      previousFirmwareVersion: "1.8.0",
      memberCount: 1,
      eligibleCount: 1,
      state: CohortFirmwareValidationState.AVAILABLE,
      metrics: [
        create(CohortFirmwareValidationMetricSchema, {
          measurementType: MeasurementType.HASHRATE,
          baselineAverage: 90,
          targetAverage: 110,
          percentageDelta: 22.2,
          baselineReportingDeviceCount: 1,
          targetReportingDeviceCount: 1,
        }),
      ],
    }),
    create(CohortFirmwareValidationBaselineSchema, {
      previousFirmwareVersion: "1.9.0",
      memberCount: 3,
      eligibleCount: 2,
      state: CohortFirmwareValidationState.AVAILABLE,
      metrics: [
        create(CohortFirmwareValidationMetricSchema, {
          measurementType: MeasurementType.HASHRATE,
          baselineAverage: 100,
          targetAverage: 110,
          percentageDelta: 10,
          baselineReportingDeviceCount: 2,
          targetReportingDeviceCount: 2,
        }),
      ],
    }),
  ],
});

describe("FirmwareValidationSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getFirmwareValidation.mockResolvedValue(availableResponse);
  });

  it("selects a target, previous version, and comparison window", async () => {
    render(<FirmwareValidationSection cohort={cohort} />);

    await waitFor(() =>
      expect(mocks.getFirmwareValidation).toHaveBeenCalledWith(
        expect.objectContaining({
          cohortId: 42n,
          manufacturer: "Proto",
          model: "Rig",
          comparisonWindow: CohortFirmwareValidationWindow.SIX_HOURS,
        }),
      ),
    );
    expect(await screen.findByText(/Comparing/)).toHaveTextContent("1.9.0");
    expect(screen.getByText("Preliminary")).toBeInTheDocument();
    expect(screen.getByText("2/2 baseline · 2/2 target reporting")).toBeInTheDocument();

    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Previous firmware" }), "1.8.0");
    expect(screen.getByText(/Comparing/)).toHaveTextContent("1.8.0");

    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "Comparison window" }),
      String(CohortFirmwareValidationWindow.TWENTY_FOUR_HOURS),
    );
    await waitFor(() =>
      expect(mocks.getFirmwareValidation).toHaveBeenLastCalledWith(
        expect.objectContaining({ comparisonWindow: CohortFirmwareValidationWindow.TWENTY_FOUR_HOURS }),
      ),
    );

    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Miner target" }), "bitmain:::s21");
    await waitFor(() =>
      expect(mocks.getFirmwareValidation).toHaveBeenLastCalledWith(
        expect.objectContaining({ manufacturer: "Bitmain", model: "S21" }),
      ),
    );
  });

  it("renders a dedicated expired-history state", async () => {
    mocks.getFirmwareValidation.mockResolvedValue(
      create(GetCohortFirmwareValidationResponseSchema, {
        state: CohortFirmwareValidationState.HISTORY_EXPIRED,
        manufacturer: "Proto",
        model: "Rig",
        comparisonWindow: CohortFirmwareValidationWindow.SIX_HOURS,
      }),
    );

    render(<FirmwareValidationSection cohort={cohort} />);

    expect(await screen.findByText("The baseline is outside telemetry retention")).toBeInTheDocument();
  });
});
