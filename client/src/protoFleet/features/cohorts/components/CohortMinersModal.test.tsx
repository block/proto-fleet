import type { ComponentProps } from "react";
import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import CohortMinersModal from "./CohortMinersModal";
import {
  CohortConfigDimension,
  CohortConfigLifecycleState,
  CohortConfigStatusSchema,
  CohortDeviceDisplaySchema,
  CohortDeviceSchema,
  CohortFirmwareRolloutState,
  CohortFirmwareStatusSchema,
  CohortPoolDesiredConfigSchema,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";

const mocks = vi.hoisted(() => ({
  listDevices: vi.fn(),
  miningPools: [
    { poolId: "1", name: "Primary pool", poolUrl: "stratum+tcp://primary.example", username: "worker" },
    { poolId: "2", name: "Backup pool", poolUrl: "stratum+tcp://backup.example", username: "worker" },
  ],
}));

vi.mock("@/protoFleet/api/useCohortApi", () => ({
  useCohortApi: () => ({ listDevices: mocks.listDevices }),
}));

vi.mock("@/protoFleet/api/usePools", () => ({
  default: () => ({ miningPools: mocks.miningPools, isLoading: false }),
}));

const desiredPools = create(CohortPoolDesiredConfigSchema, {
  primaryPoolId: 1n,
  backup1PoolId: 2n,
});

const buildDevice = ({
  identifier = "miner-001",
  firmwareState = CohortFirmwareRolloutState.COMPLETE,
  poolState = CohortConfigLifecycleState.CONVERGED,
}: {
  identifier?: string;
  firmwareState?: CohortFirmwareRolloutState;
  poolState?: CohortConfigLifecycleState;
} = {}) =>
  create(CohortDeviceSchema, {
    deviceIdentifier: identifier,
    display: create(CohortDeviceDisplaySchema, {
      name: "Validation miner",
      workerName: "worker-01",
      manufacturer: "Proto",
      model: "Rig",
      ipAddress: "192.0.2.10",
      firmwareVersion: "1.3.5",
    }),
    firmwareStatus: create(CohortFirmwareStatusSchema, {
      currentFirmwareVersion: "1.3.5",
      targetFirmwareFileId: "fw-1",
      targetFirmwareVersion: "1.3.6",
      state: firmwareState,
    }),
    configStatuses: [
      create(CohortConfigStatusSchema, {
        dimension: CohortConfigDimension.POOLS,
        supported: true,
        state: poolState,
      }),
    ],
  });

const result = (devices = [buildDevice()], nextPageToken = "", totalCount = devices.length) => ({
  devices,
  nextPageToken,
  totalCount,
  availableCount: 0,
  reservedCount: totalCount,
});

const renderModal = (props: Partial<ComponentProps<typeof CohortMinersModal>> = {}) =>
  render(
    <CohortMinersModal
      open
      cohortId={7n}
      cohortLabel="Validation cohort"
      desiredPools={desiredPools}
      onDismiss={vi.fn()}
      {...props}
    />,
  );

describe("CohortMinersModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listDevices.mockResolvedValue(result());
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("loads read-only miner desired state and reconciliation status through the cohort filter", async () => {
    renderModal();

    expect(await screen.findByText("Validation miner")).toBeInTheDocument();
    expect(screen.getByText(/Proto Rig/)).toBeInTheDocument();
    expect(screen.getByText("1.3.5")).toBeInTheDocument();
    expect(screen.getByText("Target: 1.3.6")).toBeInTheDocument();
    expect(screen.getByText("Primary pool")).toBeInTheDocument();
    expect(screen.getByText("Backup: Backup pool")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Firmware: Complete" })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Pools: Complete" })).toBeInTheDocument();
    expect(mocks.listDevices).toHaveBeenCalledWith({
      pageSize: 50,
      pageToken: "",
      filter: { cohortIds: [7n], search: "" },
    });
  });

  it("shows a loading state while the current page is pending", async () => {
    let resolveRequest: ((value: ReturnType<typeof result>) => void) | undefined;
    mocks.listDevices.mockReturnValue(
      new Promise((resolve) => {
        resolveRequest = resolve;
      }),
    );

    renderModal();

    expect(await screen.findByTestId("cohort-miners-loading")).toBeInTheDocument();
    await act(async () => resolveRequest?.(result()));
    expect(await screen.findByText("Validation miner")).toBeInTheDocument();
  });

  it("paginates and resets to the first page when search changes", async () => {
    mocks.listDevices.mockImplementation(
      async ({ pageToken, filter }: { pageToken?: string; filter?: { search?: string } }) => {
        if (filter?.search) return result([buildDevice({ identifier: `search-${filter.search}` })]);
        if (pageToken === "page-2") return result([buildDevice({ identifier: "miner-051" })], "", 51);
        return result([buildDevice()], "page-2", 51);
      },
    );
    renderModal();

    await screen.findByText("Showing 1-1 of 51 miners");
    await userEvent.click(screen.getByRole("button", { name: "Next miners page" }));
    expect(await screen.findByText("Showing 51-51 of 51 miners")).toBeInTheDocument();
    expect(mocks.listDevices).toHaveBeenLastCalledWith(
      expect.objectContaining({ pageToken: "page-2", filter: { cohortIds: [7n], search: "" } }),
    );

    await userEvent.type(screen.getByLabelText("Search miners"), "rack");
    await waitFor(() =>
      expect(mocks.listDevices).toHaveBeenLastCalledWith(
        expect.objectContaining({ pageToken: "", filter: { cohortIds: [7n], search: "rack" } }),
      ),
    );
    expect(screen.getByRole("button", { name: "Previous miners page" })).toBeDisabled();
  });

  it("refreshes a converging visible page without losing its page token", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const convergingDevice = buildDevice({ firmwareState: CohortFirmwareRolloutState.VERIFYING });
    mocks.listDevices.mockImplementation(async ({ pageToken }: { pageToken?: string }) =>
      pageToken === "page-2" ? result([convergingDevice], "", 51) : result([convergingDevice], "page-2", 51),
    );
    renderModal();

    await screen.findByText("Showing 1-1 of 51 miners");
    await userEvent.click(screen.getByRole("button", { name: "Next miners page" }));
    await screen.findByText("Showing 51-51 of 51 miners");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });

    await waitFor(() => expect(mocks.listDevices).toHaveBeenCalledTimes(3));
    expect(mocks.listDevices).toHaveBeenLastCalledWith(
      expect.objectContaining({ pageToken: "page-2", filter: { cohortIds: [7n], search: "" } }),
    );
  });

  it("renders empty and error states with a retry path", async () => {
    mocks.listDevices.mockResolvedValueOnce(result([]));
    const { rerender } = renderModal();
    expect(await screen.findByText("No miners in this cohort.")).toBeInTheDocument();

    mocks.listDevices.mockRejectedValueOnce(new Error("failed")).mockResolvedValueOnce(result());
    rerender(
      <CohortMinersModal
        open
        cohortId={8n}
        cohortLabel="Other cohort"
        desiredPools={desiredPools}
        onDismiss={vi.fn()}
      />,
    );
    expect(await screen.findByTestId("cohort-miners-error")).toHaveTextContent("Couldn't load cohort miners");
    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByText("Validation miner")).toBeInTheDocument();
  });

  it("uses the same cohort filter for implicit default-cohort miners", async () => {
    mocks.listDevices.mockResolvedValue(result([buildDevice({ identifier: "implicit-default-miner" })], "", 1));

    renderModal({ cohortId: 1n, cohortLabel: "Default cohort", desiredPools: undefined });

    expect(await screen.findByText("Validation miner")).toBeInTheDocument();
    expect(mocks.listDevices).toHaveBeenCalledWith(
      expect.objectContaining({ filter: { cohortIds: [1n], search: "" } }),
    );
    expect(screen.getAllByText("Not enforced").length).toBeGreaterThan(0);
  });
});
