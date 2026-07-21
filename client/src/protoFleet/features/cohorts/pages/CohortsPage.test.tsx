import { MemoryRouter } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  CohortSummarySchema,
  CohortTelemetryComparisonWindow,
  GetCohortTelemetryComparisonResponseSchema,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import CohortsPage from "@/protoFleet/features/cohorts/pages/CohortsPage";

const mocks = vi.hoisted(() => ({
  listAllCohorts: vi.fn(),
  getTelemetryComparison: vi.fn(),
  releaseCohort: vi.fn(),
  listFirmwareFiles: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("@/protoFleet/api/useCohortApi", () => ({
  useCohortApi: () => ({
    listAllCohorts: mocks.listAllCohorts,
    getTelemetryComparison: mocks.getTelemetryComparison,
    releaseCohort: mocks.releaseCohort,
  }),
}));

vi.mock("@/protoFleet/api/useFirmwareApi", () => ({
  useFirmwareApi: () => ({ listFirmwareFiles: mocks.listFirmwareFiles }),
}));

vi.mock("@/protoFleet/routing/siteScope", () => ({
  scopedPath: (path: string) => path,
  useRouteSiteScope: () => undefined,
}));

vi.mock("@/shared/hooks/useNavigate", () => ({
  useNavigate: () => mocks.navigate,
}));

vi.mock("@/protoFleet/store", () => ({
  useRole: () => "USER",
  useUsername: () => "operator",
}));

vi.mock("@/protoFleet/features/cohorts/components/CohortModal", () => ({
  default: () => null,
}));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listAllCohorts.mockResolvedValue([
    create(CohortSummarySchema, {
      id: 1n,
      label: "Default",
      isDefault: true,
      purpose: "Default cohort",
      explicitMemberCount: 8n,
    }),
    create(CohortSummarySchema, {
      id: 42n,
      label: "Firmware canary",
      purpose: "Validate firmware 2.0",
      explicitMemberCount: 2n,
      ownerUsername: "operator",
    }),
  ]);
  mocks.listFirmwareFiles.mockResolvedValue([]);
  mocks.getTelemetryComparison.mockResolvedValue(
    create(GetCohortTelemetryComparisonResponseSchema, {
      comparisonWindow: CohortTelemetryComparisonWindow.SIX_HOURS,
    }),
  );
});

afterEach(() => {
  vi.useRealTimers();
});

describe("CohortsPage", () => {
  it("renders the comparison dashboard and one cohort register without the miner-assignment table", async () => {
    render(
      <MemoryRouter>
        <CohortsPage />
      </MemoryRouter>,
    );

    expect(await screen.findByText("Fleet allocation")).toBeInTheDocument();
    expect(screen.getByText("Cohort register")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Firmware canary" })).toBeInTheDocument();
    expect(screen.queryByText("Miner assignments")).not.toBeInTheDocument();
    expect(screen.queryByText("My cohorts")).not.toBeInTheDocument();

    await waitFor(() => {
      expect(mocks.getTelemetryComparison).toHaveBeenCalledWith(
        expect.objectContaining({
          cohortIds: [1n, 42n],
          comparisonWindow: CohortTelemetryComparisonWindow.SIX_HOURS,
        }),
      );
    });
  });
});
