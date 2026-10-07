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
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";

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

  it.each(["1.2.3", ""])("renders read-only firmware %j without an action or tooltip", async (firmwareVersion) => {
    const user = userEvent.setup();
    render(<MinerFirmware miner={createMockMiner({ firmwareVersion })} />);

    const label = screen.getByText(firmwareVersion || "Unknown");
    await user.hover(label);
    await user.click(label);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
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
  ])("opens history with %s and dismisses the tooltip", async (_, key) => {
    const user = userEvent.setup();
    const onViewHistory = vi.fn();
    render(<MinerFirmware miner={createMockMiner()} onViewHistory={onViewHistory} />);

    await user.tab();
    expect(screen.getByRole("button")).toHaveFocus();
    expect(screen.getByRole("tooltip")).toBeVisible();
    await user.keyboard(key);
    expect(onViewHistory).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });

  it("shows a tooltip on hover and dismisses it when the pointer leaves", async () => {
    const user = userEvent.setup();
    render(<MinerFirmware miner={createMockMiner()} onViewHistory={vi.fn()} />);

    const button = screen.getByRole("button");
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    await user.hover(button);
    expect(screen.getByRole("tooltip")).toBeVisible();
    expect(screen.getByRole("tooltip")).toHaveTextContent("View firmware history");
    await user.unhover(button);
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });

  it("shows the tooltip on focus and dismisses it on blur", async () => {
    const user = userEvent.setup();
    render(
      <>
        <MinerFirmware miner={createMockMiner()} onViewHistory={vi.fn()} />
        <button type="button">Next action</button>
      </>,
    );

    await user.tab();
    expect(screen.getByRole("tooltip")).toBeVisible();
    expect(screen.getByRole("button", { name: "View firmware history for Test miner" })).toHaveAttribute(
      "aria-describedby",
      screen.getByRole("tooltip").id,
    );
    await user.tab();
    expect(screen.getByRole("button", { name: "Next action" })).toHaveFocus();
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });

  it("dismisses the tooltip when history opens", async () => {
    const user = userEvent.setup();
    const onViewHistory = vi.fn();
    render(<MinerFirmware miner={createMockMiner()} onViewHistory={onViewHistory} />);

    const button = screen.getByRole("button");
    await user.hover(button);
    expect(screen.getByRole("tooltip")).toBeVisible();
    await user.click(button);
    expect(onViewHistory).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  });

  it.each(["hover", "focus"])("dismisses a tooltip opened by %s with Escape", async (activation) => {
    const user = userEvent.setup();
    const onViewHistory = vi.fn();
    render(<MinerFirmware miner={createMockMiner()} onViewHistory={onViewHistory} />);

    if (activation === "hover") {
      await user.hover(screen.getByRole("button"));
    } else {
      await user.tab();
    }
    expect(screen.getByRole("tooltip")).toBeVisible();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    expect(onViewHistory).not.toHaveBeenCalled();
  });

  it("releases Escape dismissal when history permission is lost while the tooltip is open", async () => {
    const user = userEvent.setup();
    const onDismissParent = vi.fn();
    const onViewHistory = vi.fn();
    const miner = createMockMiner();
    const Harness = ({ canViewHistory }: { canViewHistory: boolean }) => {
      useEscapeDismiss(onDismissParent);
      return <MinerFirmware miner={miner} onViewHistory={canViewHistory ? onViewHistory : undefined} />;
    };
    const { rerender } = render(<Harness canViewHistory />);

    await user.hover(screen.getByRole("button"));
    expect(screen.getByRole("tooltip")).toBeVisible();
    rerender(<Harness canViewHistory={false} />);
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();

    await user.keyboard("{Escape}");
    expect(onDismissParent).toHaveBeenCalledTimes(1);
    expect(onViewHistory).not.toHaveBeenCalled();
  });
});
