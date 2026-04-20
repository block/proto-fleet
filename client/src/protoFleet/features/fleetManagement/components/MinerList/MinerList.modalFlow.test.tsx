import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import MinerList from "./MinerList";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

let minersById: Record<string, { pairingStatus: PairingStatus; deviceStatus: DeviceStatus }> = {};

vi.mock("@/protoFleet/store", () => ({
  useUsername: () => "",
}));

vi.mock("./minerColConfig", () => ({
  default: ({ onOpenStatusFlow }: { onOpenStatusFlow: (deviceIdentifier: string) => void }) => ({
    status: {
      width: "min-w-48",
      component: (device: { deviceIdentifier: string }) => (
        <button data-testid="open-status-flow" onClick={() => onOpenStatusFlow(device.deviceIdentifier)}>
          Open status flow
        </button>
      ),
    },
  }),
}));

vi.mock("@/shared/components/List", () => ({
  default: ({ items, colConfig }: any) => (
    <div>{items?.[0] ? colConfig.status?.component?.(items[0], []) : <div data-testid="empty-list" />}</div>
  ),
}));

vi.mock("@/protoFleet/features/auth/components/AuthenticateMiners", () => ({
  AuthenticateMiners: ({ open, onClose }: { open?: boolean; onClose: () => void }) =>
    open ? (
      <div data-testid="authenticate-miners">
        <button onClick={onClose} data-testid="authenticate-miners-close">
          Close
        </button>
      </div>
    ) : null,
}));

vi.mock("@/protoFleet/features/auth/components/AuthenticateFleetModal", () => ({
  default: ({
    open,
    onAuthenticated,
    onDismiss,
  }: {
    open?: boolean;
    onAuthenticated: (username: string, password: string) => void;
    onDismiss: () => void;
  }) =>
    open ? (
      <div data-testid="authenticate-fleet-modal">
        <button onClick={() => onAuthenticated("testuser", "testpass")} data-testid="authenticate-fleet-success">
          Authenticate
        </button>
        <button onClick={onDismiss} data-testid="authenticate-fleet-close">
          Close
        </button>
      </div>
    ) : null,
}));

vi.mock("@/protoFleet/features/fleetManagement/components/ActionBar/SettingsWidget/PoolSelectionPage", () => ({
  default: ({ open, onDismiss }: { open?: boolean; onDismiss: () => void }) =>
    open ? (
      <div data-testid="pool-selection-page">
        <button onClick={onDismiss} data-testid="pool-selection-close">
          Close
        </button>
      </div>
    ) : null,
}));

vi.mock("@/protoFleet/components/StatusModal", () => ({
  ProtoFleetStatusModal: ({ open, onClose }: { open?: boolean; onClose: () => void }) =>
    open ? (
      <div data-testid="status-modal">
        <button onClick={onClose} data-testid="status-modal-close">
          Close
        </button>
      </div>
    ) : null,
}));

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: () => ({ isPhone: false }),
}));

vi.mock("@/shared/hooks/useReactiveLocalStorage", () => ({
  useReactiveLocalStorage: () => [false],
}));

const renderMinerList = () =>
  render(
    <MemoryRouter>
      <MinerList
        title="Miners"
        minerIds={["miner-1"]}
        miners={minersById as any}
        errorsByDevice={{}}
        errorsLoaded={true}
        getActiveBatches={() => []}
        totalMiners={1}
        onAddMiners={vi.fn()}
      />
    </MemoryRouter>,
  );

describe("MinerList modal flow orchestration", () => {
  beforeEach(() => {
    minersById = {
      "miner-1": {
        pairingStatus: PairingStatus.PAIRED,
        deviceStatus: DeviceStatus.ONLINE,
      },
    };
  });

  it("opens AuthenticateMiners for auth-needed miners", async () => {
    const user = userEvent.setup();
    minersById["miner-1"] = {
      pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
      deviceStatus: DeviceStatus.ONLINE,
    };

    renderMinerList();
    await user.click(screen.getByTestId("open-status-flow"));

    expect(screen.getByTestId("authenticate-miners")).toBeInTheDocument();
  });

  it("opens fleet auth then pool selection for needs-mining-pool miners and resets on close", async () => {
    const user = userEvent.setup();
    minersById["miner-1"] = {
      pairingStatus: PairingStatus.PAIRED,
      deviceStatus: DeviceStatus.NEEDS_MINING_POOL,
    };

    renderMinerList();
    await user.click(screen.getByTestId("open-status-flow"));
    expect(screen.getByTestId("authenticate-fleet-modal")).toBeInTheDocument();

    await user.click(screen.getByTestId("authenticate-fleet-success"));
    expect(screen.getByTestId("pool-selection-page")).toBeInTheDocument();

    await user.click(screen.getByTestId("pool-selection-close"));
    expect(screen.queryByTestId("authenticate-fleet-modal")).not.toBeInTheDocument();
    expect(screen.queryByTestId("pool-selection-page")).not.toBeInTheDocument();
  });

  it("opens status modal for non-auth, non-pool miners and closes cleanly", async () => {
    const user = userEvent.setup();
    minersById["miner-1"] = {
      pairingStatus: PairingStatus.PAIRED,
      deviceStatus: DeviceStatus.ONLINE,
    };

    renderMinerList();
    await user.click(screen.getByTestId("open-status-flow"));
    expect(screen.getByTestId("status-modal")).toBeInTheDocument();

    await user.click(screen.getByTestId("status-modal-close"));
    expect(screen.queryByTestId("status-modal")).not.toBeInTheDocument();
  });
});
