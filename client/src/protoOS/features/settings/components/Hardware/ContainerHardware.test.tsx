import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import { ContainerHardwareView } from "./ContainerHardware";
import { containerInventory } from "./containerInventory";
import { tanksFixture } from "@/protoOS/features/tanks/Tanks.fixtures";
import { asDeviceId, type SystemInfoData } from "@/protoOS/store/contract";

const systemInfo: SystemInfoData = {
  identity: { deviceId: asDeviceId("device:ct1"), deviceType: "container", adapterId: "mdk-v2", macAddresses: [] },
  moduleModel: "ct1",
  controlBoard: { model: "Control Server", serialNumber: "CB-001", machineName: "Primary" },
  operatingSystem: { version: "2.0.0" },
};

describe("Container hardware settings", () => {
  test("keeps unavailable inventory visible as dashes without a load-error message", () => {
    render(<ContainerHardwareView />);
    expect(screen.getAllByText("—")).toHaveLength(13);
    expect(screen.getByRole("button", { name: "Hashboards —" })).toBeDisabled();
    expect(screen.queryByText(/Could not load|try again/i)).not.toBeInTheDocument();
  });
  test("counts the entire container and opens a scrollable hashboard inventory", () => {
    const { hardware } = tanksFixture(6, 16);
    render(<ContainerHardwareView hardware={hardware} />);
    expect(screen.getByRole("button", { name: "Tanks 6 units" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Modules 96 units" })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "Hashboards 288 units" }));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getAllByRole("row")).toHaveLength(289);
    expect(within(dialog).getByText("T1-M01-1")).toBeInTheDocument();
    expect(within(dialog).getByText("T6-M16-3")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Close dialog" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  test("does not invent inventory counts from the design or plant readings", () => {
    const { hardware } = tanksFixture(1, 1, true);
    render(<ContainerHardwareView hardware={hardware} />);
    for (const name of [
      "PSUs",
      "Controllers",
      "CDU",
      "Coolant pumps",
      "Dry cooler fans",
      "PDUs",
      "A/C",
      "Lights",
      "Control cabinet",
      "Alarm",
    ]) {
      expect(screen.getByRole("button", { name: `${name} —` })).toBeDisabled();
    }
  });

  test("shows the responding controller identity without claiming a second controller", () => {
    const { hardware } = tanksFixture(1, 1);
    render(<ContainerHardwareView hardware={hardware} systemInfo={systemInfo} />);
    fireEvent.click(screen.getByRole("button", { name: "Controllers 1 unit" }));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText("CB-001")).toBeInTheDocument();
    expect(within(dialog).getByText("2.0.0")).toBeInTheDocument();
    expect(within(dialog).getAllByRole("row")).toHaveLength(2);
  });

  test("uses reported serial numbers, never a MAC address in the serial-number column", () => {
    const { hardware } = tanksFixture(1, 1);
    const board = hardware.hashboards.values().next().value!;
    const node = hardware.topology!.hashboards.get(board.ref.id)!;
    board.identity = node.identity = { kind: "mac", value: "aa:bb:cc:dd:ee:ff" };
    board.serialNumber = "HB-001";
    board.firmware = { version: "0.12.4" };
    const rows = () => containerInventory(hardware).find((group) => group.name === "Hashboards")!.items!;
    expect(rows()[0]).toMatchObject({ serialNumber: "HB-001", firmware: "0.12.4" });
    delete board.serialNumber;
    expect(rows()[0].serialNumber).toBeUndefined();
  });
});
