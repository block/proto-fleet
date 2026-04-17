import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import MinerName from "./MinerName";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import * as useNeedsAttentionModule from "@/shared/hooks/useNeedsAttention";

vi.mock("@/shared/hooks/useNeedsAttention");

vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/SingleMinerActionsMenu", () => ({
  default: () => <div data-testid="actions-menu">Actions Menu</div>,
}));

function createMockMiner(overrides: Partial<MinerStateSnapshot> = {}): MinerStateSnapshot {
  return {
    deviceIdentifier: "test-device-id",
    name: "Test Miner",
    macAddress: "",
    serialNumber: "",
    powerUsage: [],
    temperature: [],
    hashrate: [],
    efficiency: [],
    ipAddress: "",
    url: "",
    deviceStatus: DeviceStatus.ONLINE,
    pairingStatus: PairingStatus.PAIRED,
    model: "",
    manufacturer: "",
    temperatureStatus: 0,
    firmwareVersion: "",
    groupLabels: [],
    rackLabel: "",
    driverName: "",
    workerName: "",
    ...overrides,
  } as MinerStateSnapshot;
}

describe("MinerName", () => {
  const deviceIdentifier = "test-device-id";
  const minerName = "Test Miner";

  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(false);
  });

  it("renders miner name with title attribute for tooltip", () => {
    const miner = createMockMiner();

    render(<MinerName miner={miner} errors={[]} isActionLoading={false} onOpenStatusFlow={vi.fn()} />);

    const nameElement = screen.getByTitle(minerName);
    expect(nameElement).toHaveTextContent(minerName);
  });

  it("falls back to device identifier when no custom name is set", () => {
    const miner = createMockMiner({ name: "" });

    render(<MinerName miner={miner} errors={[]} isActionLoading={false} onOpenStatusFlow={vi.fn()} />);

    expect(screen.getByTitle(deviceIdentifier)).toBeInTheDocument();
  });

  it("dims the miner name text when authentication is needed", () => {
    const miner = createMockMiner({ pairingStatus: PairingStatus.AUTHENTICATION_NEEDED });

    render(<MinerName miner={miner} errors={[]} isActionLoading={false} onOpenStatusFlow={vi.fn()} />);

    expect(screen.getByTitle("Test Miner")).toHaveClass("opacity-50");
  });

  it("does not dim the miner name text when paired", () => {
    const miner = createMockMiner({ pairingStatus: PairingStatus.PAIRED });

    render(<MinerName miner={miner} errors={[]} isActionLoading={false} onOpenStatusFlow={vi.fn()} />);

    expect(screen.getByTitle("Test Miner")).not.toHaveClass("opacity-50");
  });

  it("hides alert icon when authentication is required", () => {
    vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(true);
    const miner = createMockMiner({ pairingStatus: PairingStatus.AUTHENTICATION_NEEDED });

    render(<MinerName miner={miner} errors={[]} isActionLoading={false} onOpenStatusFlow={vi.fn()} />);

    expect(screen.queryByRole("button", { name: /view issues/i })).not.toBeInTheDocument();
  });

  it("hides alert icon when no attention is needed", () => {
    vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(false);
    const miner = createMockMiner();

    render(<MinerName miner={miner} errors={[]} isActionLoading={false} onOpenStatusFlow={vi.fn()} />);

    expect(screen.queryByRole("button", { name: /view issues/i })).not.toBeInTheDocument();
  });

  it("propagates click to row handler for navigation", async () => {
    const user = userEvent.setup();
    const rowClickHandler = vi.fn();
    const miner = createMockMiner();

    render(
      <table>
        <tbody>
          <tr onClick={rowClickHandler}>
            <td>
              <input type="checkbox" data-testid="row-checkbox" />
            </td>
            <td>
              <MinerName miner={miner} errors={[]} isActionLoading={false} onOpenStatusFlow={vi.fn()} />
            </td>
          </tr>
        </tbody>
      </table>,
    );

    await user.click(screen.getByTitle(minerName));

    expect(rowClickHandler).toHaveBeenCalledTimes(1);
    const checkbox = screen.getByTestId("row-checkbox") as HTMLInputElement;
    expect(checkbox.checked).toBe(false);
  });

  it("lets click propagate when checkbox is disabled (for row navigation)", async () => {
    const user = userEvent.setup();
    const rowClickHandler = vi.fn();
    const miner = createMockMiner();

    render(
      <table>
        <tbody>
          <tr onClick={rowClickHandler}>
            <td>
              <input type="checkbox" disabled />
            </td>
            <td>
              <MinerName miner={miner} errors={[]} isActionLoading={false} onOpenStatusFlow={vi.fn()} />
            </td>
          </tr>
        </tbody>
      </table>,
    );

    await user.click(screen.getByTitle(minerName));

    expect(rowClickHandler).toHaveBeenCalledTimes(1);
  });

  it("calls onOpenStatusFlow when the alert icon is clicked", async () => {
    const user = userEvent.setup();
    const onOpenStatusFlow = vi.fn();
    vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(true);
    const miner = createMockMiner();

    render(<MinerName miner={miner} errors={[]} isActionLoading={false} onOpenStatusFlow={onOpenStatusFlow} />);

    await user.click(screen.getByRole("button", { name: /view issues/i }));

    expect(onOpenStatusFlow).toHaveBeenCalledWith(deviceIdentifier);
  });

  it("shows spinner when action is loading", () => {
    const miner = createMockMiner();

    const { container } = render(<MinerName miner={miner} errors={[]} isActionLoading onOpenStatusFlow={vi.fn()} />);

    expect(container.querySelector(".animate-spin")).toBeInTheDocument();
  });

  it("hides spinner when no action is loading", () => {
    const miner = createMockMiner();

    const { container } = render(
      <MinerName miner={miner} errors={[]} isActionLoading={false} onOpenStatusFlow={vi.fn()} />,
    );

    expect(container.querySelector(".animate-spin")).not.toBeInTheDocument();
  });

  it("shows spinner instead of alert icon when action is loading", () => {
    vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(true);
    const miner = createMockMiner();

    const { container } = render(<MinerName miner={miner} errors={[]} isActionLoading onOpenStatusFlow={vi.fn()} />);

    expect(container.querySelector(".animate-spin")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /view issues/i })).not.toBeInTheDocument();
  });
});
