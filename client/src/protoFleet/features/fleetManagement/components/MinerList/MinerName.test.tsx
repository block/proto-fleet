import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import MinerName from "./MinerName";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import * as storeModule from "@/protoFleet/store";
import * as useNeedsAttentionModule from "@/shared/hooks/useNeedsAttention";

vi.mock("@/protoFleet/store");
vi.mock("@/shared/hooks/useNeedsAttention");

vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/SingleMinerActionsMenu", () => ({
  default: () => <div data-testid="actions-menu">Actions Menu</div>,
}));

describe("MinerName", () => {
  const deviceIdentifier = "test-device-id";
  const minerName = "Test Miner";

  beforeEach(() => {
    vi.clearAllMocks();

    vi.mocked(storeModule.useMiner).mockReturnValue({
      deviceIdentifier,
      name: minerName,
      pairingStatus: PairingStatus.PAIRED,
    } as any);
    vi.mocked(storeModule.useMinerDeviceStatus).mockReturnValue(DeviceStatus.ONLINE);
    vi.mocked(storeModule.useMinerName).mockReturnValue(minerName);
    vi.mocked(storeModule.useFleetStore).mockImplementation((selector: any) => {
      if (typeof selector === "function") {
        return selector({
          fleet: {
            selectErrorsByDevice: () => [],
          },
        });
      }
      return {};
    });
    vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(false);
  });

  it("renders miner name in a button with title attribute for tooltip", () => {
    render(<MinerName deviceIdentifier={deviceIdentifier} onOpenStatusFlow={vi.fn()} />);

    const nameButton = screen.getByRole("button", { name: minerName });
    expect(nameButton).toHaveAttribute("title", minerName);
  });

  it("falls back to device identifier when no custom name is set", () => {
    vi.mocked(storeModule.useMinerName).mockReturnValue("");

    render(<MinerName deviceIdentifier={deviceIdentifier} onOpenStatusFlow={vi.fn()} />);

    expect(screen.getByRole("button", { name: deviceIdentifier })).toBeInTheDocument();
  });

  it("hides alert icon when authentication is required", () => {
    vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(true);
    vi.mocked(storeModule.useMiner).mockReturnValue({
      deviceIdentifier,
      name: minerName,
      pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
    } as any);

    render(<MinerName deviceIdentifier={deviceIdentifier} onOpenStatusFlow={vi.fn()} />);

    expect(screen.queryByRole("button", { name: /view issues/i })).not.toBeInTheDocument();
  });

  it("calls onOpenStatusFlow when alert icon is clicked", async () => {
    const user = userEvent.setup();
    const onOpenStatusFlow = vi.fn();
    vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(true);

    render(<MinerName deviceIdentifier={deviceIdentifier} onOpenStatusFlow={onOpenStatusFlow} />);

    await user.click(screen.getByRole("button", { name: /view issues/i }));

    expect(onOpenStatusFlow).toHaveBeenCalledTimes(1);
    expect(onOpenStatusFlow).toHaveBeenCalledWith(deviceIdentifier);
  });

  it("hides alert icon when no attention is needed", () => {
    vi.mocked(useNeedsAttentionModule.useNeedsAttention).mockReturnValue(false);

    render(<MinerName deviceIdentifier={deviceIdentifier} onOpenStatusFlow={vi.fn()} />);

    expect(screen.queryByRole("button", { name: /view issues/i })).not.toBeInTheDocument();
  });
});
