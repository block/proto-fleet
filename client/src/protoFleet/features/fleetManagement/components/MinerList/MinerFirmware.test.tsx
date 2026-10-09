import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";
import MinerFirmware from "./MinerFirmware";
import {
  type MinerStateSnapshot,
  MinerStateSnapshotSchema,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

function createMockMiner(overrides: Partial<MinerStateSnapshot> = {}): MinerStateSnapshot {
  return {
    ...create(MinerStateSnapshotSchema, {
      deviceIdentifier: "test-device",
      name: "Test miner",
      firmwareVersion: "1.2.3",
      deviceStatus: DeviceStatus.ONLINE,
      pairingStatus: PairingStatus.PAIRED,
    }),
    ...overrides,
  };
}

describe("MinerFirmware", () => {
  it.each(["1.2.3", "2024.01.15", "v1.0.0-beta"])("renders firmware version %s", (firmwareVersion) => {
    render(<MinerFirmware miner={createMockMiner({ firmwareVersion })} />);

    expect(screen.getByText(firmwareVersion)).toBeInTheDocument();
  });

  it.each(["", "   ", undefined])("keeps unknown version %j actionable", async (firmwareVersion) => {
    const user = userEvent.setup();
    const onViewHistory = vi.fn();
    render(<MinerFirmware miner={createMockMiner({ firmwareVersion })} onViewHistory={onViewHistory} />);

    const button = screen.getByRole("button", { name: "View firmware history for Test miner" });
    expect(button).toHaveTextContent("Unknown");
    await user.click(within(button).getByText("Unknown"));
    expect(onViewHistory).toHaveBeenCalledTimes(1);
  });

  it.each(["1.2.3", ""])("renders read-only firmware %j without an action", async (firmwareVersion) => {
    const user = userEvent.setup();
    render(<MinerFirmware miner={createMockMiner({ firmwareVersion })} />);

    const label = screen.getByText(firmwareVersion || "Unknown");
    await user.click(label);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("opens history from the firmware version and identifies the dialog action", async () => {
    const user = userEvent.setup();
    const onViewHistory = vi.fn();
    render(<MinerFirmware miner={createMockMiner()} onViewHistory={onViewHistory} />);

    const button = screen.getByRole("button", { name: "View firmware history for Test miner" });
    expect(button).toHaveAttribute("type", "button");
    expect(button).toHaveAttribute("aria-haspopup", "dialog");
    await user.click(within(button).getByText("1.2.3"));
    expect(onViewHistory).toHaveBeenCalledTimes(1);
  });

  it("uses the device identifier in the accessible name when the miner has no name", () => {
    render(<MinerFirmware miner={createMockMiner({ name: "" })} onViewHistory={vi.fn()} />);

    expect(screen.getByRole("button", { name: "View firmware history for test-device" })).toBeInTheDocument();
  });

  it("keeps an offline miner needing authentication actionable", async () => {
    const user = userEvent.setup();
    const onViewHistory = vi.fn();
    render(
      <MinerFirmware
        miner={createMockMiner({
          deviceStatus: DeviceStatus.OFFLINE,
          pairingStatus: PairingStatus.AUTHENTICATION_NEEDED,
          firmwareVersion: "",
        })}
        onViewHistory={onViewHistory}
      />,
    );

    const button = screen.getByRole("button", { name: "View firmware history for Test miner" });
    expect(button).toBeEnabled();
    await user.click(button);
    expect(onViewHistory).toHaveBeenCalledTimes(1);
  });

  it.each([
    ["Enter", "{Enter}"],
    ["Space", " "],
  ])("opens history with %s", async (_, key) => {
    const user = userEvent.setup();
    const onViewHistory = vi.fn();
    render(<MinerFirmware miner={createMockMiner()} onViewHistory={onViewHistory} />);

    await user.tab();
    expect(screen.getByRole("button")).toHaveFocus();
    await user.keyboard(key);
    expect(onViewHistory).toHaveBeenCalledTimes(1);
  });
});
