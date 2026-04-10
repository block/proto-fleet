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

  it("renders miner name in a button with title attribute for tooltip", () => {
    const miner = createMockMiner();

    render(<MinerName miner={miner} errors={[]} onOpenStatusFlow={vi.fn()} />);

    const nameButton = screen.getByRole("button", { name: minerName });
    expect(nameButton).toHaveAttribute("title", minerName);
  });

  it("falls back to device identifier when no custom name is set", () => {
    const miner = createMockMiner({ name: "" });

    render(<MinerName miner={miner} errors={[]} onOpenStatusFlow={vi.fn()} />);

    expect(screen.getByRole("button", { name: deviceIdentifier })).toBeInTheDocument();
  });

  it("hides alert icon when authentication is required", () => {
    vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(true);
    const miner = createMockMiner({ pairingStatus: PairingStatus.AUTHENTICATION_NEEDED });

    render(<MinerName miner={miner} errors={[]} onOpenStatusFlow={vi.fn()} />);

    expect(screen.queryByRole("button", { name: /view issues/i })).not.toBeInTheDocument();
  });

  it("hides alert icon when no attention is needed", () => {
    vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(false);
    const miner = createMockMiner();

    render(<MinerName miner={miner} errors={[]} onOpenStatusFlow={vi.fn()} />);

    expect(screen.queryByRole("button", { name: /view issues/i })).not.toBeInTheDocument();
  });

  it("toggles checkbox and stops propagation when name is clicked with enabled checkbox", async () => {
    const user = userEvent.setup();
    const miner = createMockMiner();

    render(
      <table>
        <tbody>
          <tr>
            <td>
              <input type="checkbox" data-testid="row-checkbox" />
            </td>
            <td>
              <MinerName miner={miner} errors={[]} onOpenStatusFlow={vi.fn()} />
            </td>
          </tr>
        </tbody>
      </table>,
    );

    const checkbox = screen.getByTestId("row-checkbox") as HTMLInputElement;
    expect(checkbox.checked).toBe(false);

    await user.click(screen.getByRole("button", { name: minerName }));

    expect(checkbox.checked).toBe(true);
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
              <MinerName miner={miner} errors={[]} onOpenStatusFlow={vi.fn()} />
            </td>
          </tr>
        </tbody>
      </table>,
    );

    await user.click(screen.getByRole("button", { name: minerName }));

    expect(rowClickHandler).toHaveBeenCalledTimes(1);
  });

  it("calls onOpenStatusFlow when the alert icon is clicked", async () => {
    const user = userEvent.setup();
    const onOpenStatusFlow = vi.fn();
    vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(true);
    const miner = createMockMiner();

    render(<MinerName miner={miner} errors={[]} onOpenStatusFlow={onOpenStatusFlow} />);

    await user.click(screen.getByRole("button", { name: /view issues/i }));

    expect(onOpenStatusFlow).toHaveBeenCalledWith(deviceIdentifier);
  });
});
